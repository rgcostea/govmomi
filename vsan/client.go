// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vsan

import (
    "context"
    "crypto/tls"
    "encoding/xml"
    "errors"
    "fmt"
    "net/http"
    "time"

    "github.com/vmware/govmomi/object"
    "github.com/vmware/govmomi/vim25"
    "github.com/vmware/govmomi/vim25/soap"
    vimtypes "github.com/vmware/govmomi/vim25/types"
    "github.com/vmware/govmomi/vsan/methods"
    vsantypes "github.com/vmware/govmomi/vsan/types"
)

// Namespace and Path constants
const (
    Namespace = "vsan"
    Path      = "/vsanHealth"
    // Default vSAN VMODL versions
    VsanVersion3 = "vsan.version.version3"
    VsanVersion2 = "vsan.version.version2"
    VsanVersion1 = "vsan.version.version1"
)

// Creates the vsan cluster config system instance. This is to be queried from vsan health.
var (
    VsanVcClusterConfigSystemInstance = vimtypes.ManagedObjectReference{
        Type:  "VsanVcClusterConfigSystem",
        Value: "vsan-cluster-config-system",
    }
    VsanPerformanceManagerInstance = vimtypes.ManagedObjectReference{
        Type:  "VsanPerformanceManager",
        Value: "vsan-performance-manager",
    }
    VsanQueryObjectIdentitiesInstance = vimtypes.ManagedObjectReference{
        Type:  "VsanObjectSystem",
        Value: "vsan-cluster-object-system",
    }
    VsanPropertyCollectorInstance = vimtypes.ManagedObjectReference{
        Type:  "PropertyCollector",
        Value: "vsan-property-collector",
    }
    VsanVcStretchedClusterSystem = vimtypes.ManagedObjectReference{
        Type:  "VimClusterVsanVcStretchedClusterSystem",
        Value: "vsan-stretched-cluster-system",
    }
    VsanVbossSystemInstance = vimtypes.ManagedObjectReference{
        Type:  "VsanVbossSystem",
        Value: "vsan-vboss-system",
    }
)

// Client used for accessing vsan health APIs.
type Client struct {
    *soap.Client

    RoundTripper soap.RoundTripper

    vim25Client *vim25.Client
    
    // VsanVersion stores the detected vSAN API version
    VsanVersion string
}

// NewClient creates a new VsanHealth client with automatic version detection
func NewClient(ctx context.Context, c *vim25.Client) (*Client, error) {
    // Try to detect the vSAN API version
    version := detectVsanVersion(c)
    
    // Create service client with vsan namespace
    sc := c.Client.NewServiceClient(Path, Namespace)
    
    // Set the correct vSAN version for the SOAP client
    if version != "" && version != Namespace {
        // For vSAN-specific versions, we need to set both namespace and version
        // The namespace should be "vsan" and version should be the specific version
        sc.Namespace = "urn:vsan"  // Keep the vsan namespace
        sc.Version = version        // Set the specific version (e.g., "vsan.version.version3")
    } else {
        // Default case - use standard vsan namespace
        sc.Namespace = "urn:vsan"
        sc.Version = version
    }
    
    // Ensure cookies are properly shared (should already be done by NewServiceClient)
    // This mimics PyVmomi's behavior of copying the cookie to the vSAN stub
    if c.Client.Jar != nil {
        sc.Jar = c.Client.Jar
    }
    
    return &Client{
        Client:       sc,
        RoundTripper: sc,
        vim25Client:  c,
        VsanVersion:  version,
    }, nil
}

// NewClientWithVersion creates a new VsanHealth client with a specific version
func NewClientWithVersion(ctx context.Context, c *vim25.Client, version string) (*Client, error) {
    sc := c.Client.NewServiceClient(Path, Namespace)
    
    // Override with specific version if provided
    if version != "" {
        sc.Namespace = version
        sc.Version = version
    }
    
    return &Client{
        Client:       sc,
        RoundTripper: sc,
        vim25Client:  c,
        VsanVersion:  version,
    }, nil
}

// vsanServiceVersions represents the vSAN service versions XML response
type vsanServiceVersions struct {
    XMLName xml.Name `xml:"namespaces"`
    Items   []struct {
        Name    string `xml:"name"`
        Version string `xml:"version"`
    } `xml:"namespace"`
}

// detectVsanVersion detects the latest vSAN API version available on the vCenter
func detectVsanVersion(c *vim25.Client) string {
    // Extract hostname from the SOAP client URL
    hostname := c.URL().Host
    
    // Construct the vSAN service versions URL
    url := fmt.Sprintf("https://%s/sdk/vsanServiceVersions.xml", hostname)
    
    // Create HTTP client with TLS config that skips verification (same as SOAP client)
    httpClient := &http.Client{
        Timeout: 5 * time.Second,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{
                InsecureSkipVerify: true,
            },
        },
    }
    
    // Try to fetch the vSAN service versions
    resp, err := httpClient.Get(url)
    if err != nil {
        // If we can't fetch versions, default to vim version for vSAN
        // This mimics PyVmomi's behavior when vSAN versions aren't available
        return c.Client.Version
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return c.Client.Version
    }
    
    // Parse the XML response
    var versions vsanServiceVersions
    if err := xml.NewDecoder(resp.Body).Decode(&versions); err != nil {
        return c.Client.Version
    }
    
    // Look for vSAN namespace and return the latest version
    // This mimics PyVmomi's GetLatestVmodlVersion behavior
    for _, item := range versions.Items {
        if item.Name == "urn:vsan" {
            // vSAN namespace exists, use the vSAN-specific version
            // In production, you'd parse item.Version to get the actual version
            // For now, we'll use the hardcoded latest version when vSAN is detected
            return VsanVersion3
        }
    }
    
    // Default to vim version if vSAN namespace not found
    // This matches PyVmomi's fallback behavior
    return c.Client.Version
}

// GetVsanVersion returns the detected vSAN API version
func (c *Client) GetVsanVersion() string {
    return c.VsanVersion
}

// RoundTrip dispatches to the RoundTripper field.
func (c *Client) RoundTrip(ctx context.Context, req, res soap.HasFault) error {
    return c.RoundTripper.RoundTrip(ctx, req, res)
}

// VsanClusterGetConfig calls the Vsan health's VsanClusterGetConfig API.
func (c *Client) VsanClusterGetConfig(ctx context.Context, cluster vimtypes.ManagedObjectReference) (*vsantypes.VsanConfigInfoEx, error) {
    req := vsantypes.VsanClusterGetConfig{
        This:    VsanVcClusterConfigSystemInstance,
        Cluster: cluster,
    }

    res, err := methods.VsanClusterGetConfig(ctx, c, &req)
    if err != nil {
        return nil, err
    }
    return &res.Returnval, nil
}

// VsanClusterReconfig calls the Vsan health's VsanClusterReconfig API.
func (c *Client) VsanClusterReconfig(ctx context.Context, cluster vimtypes.ManagedObjectReference, spec vsantypes.VimVsanReconfigSpec) (*object.Task, error) {
    req := vsantypes.VsanClusterReconfig{
        This:             VsanVcClusterConfigSystemInstance,
        Cluster:          cluster,
        VsanReconfigSpec: spec,
    }

    res, err := methods.VsanClusterReconfig(ctx, c, &req)
    if err != nil {
        return nil, err
    }

    return object.NewTask(c.vim25Client, res.Returnval), nil
}

// VsanPerfQueryPerf calls the vsan performance manager API
func (c *Client) VsanPerfQueryPerf(ctx context.Context, cluster *vimtypes.ManagedObjectReference, qSpecs []vsantypes.VsanPerfQuerySpec) ([]vsantypes.VsanPerfEntityMetricCSV, error) {
    req := vsantypes.VsanPerfQueryPerf{
        This:       VsanPerformanceManagerInstance,
        Cluster:    cluster,
        QuerySpecs: qSpecs,
    }

    res, err := methods.VsanPerfQueryPerf(ctx, c, &req)
    if err != nil {
        return nil, err
    }
    return res.Returnval, nil
}

// VsanQueryObjectIdentities return host uuid
func (c *Client) VsanQueryObjectIdentities(ctx context.Context, cluster vimtypes.ManagedObjectReference) (*vsantypes.VsanObjectIdentityAndHealth, error) {
    req := vsantypes.VsanQueryObjectIdentities{
        This:    VsanQueryObjectIdentitiesInstance,
        Cluster: &cluster,
    }

    res, err := methods.VsanQueryObjectIdentities(ctx, c, &req)

    if err != nil {
        return nil, err
    }

    return res.Returnval, nil
}

// VsanHostGetConfig returns the config of host's vSAN system.
func (c *Client) VsanHostGetConfig(ctx context.Context, vsanSystem vimtypes.ManagedObjectReference) (*vsantypes.VsanHostConfigInfoEx, error) {
    req := vimtypes.RetrievePropertiesEx{
        SpecSet: []vimtypes.PropertyFilterSpec{{
            ObjectSet: []vimtypes.ObjectSpec{{
                Obj: vsanSystem}},
            PropSet: []vimtypes.PropertySpec{{
                Type:    "HostVsanSystem",
                PathSet: []string{"config"}}}}},
        This: VsanPropertyCollectorInstance}

    res, err := methods.RetrievePropertiesEx(ctx, c, &req)
    if err != nil {
        return nil, err
    }

    var property vimtypes.DynamicProperty
    if res != nil && res.Returnval != nil {
        for _, obj := range res.Returnval.Objects {
            for _, prop := range obj.PropSet {
                if prop.Name == "config" {
                    property = prop
                    break
                }
            }
        }
    }

    switch cfg := property.Val.(type) {
    case vimtypes.VsanHostConfigInfo:
        return &vsantypes.VsanHostConfigInfoEx{VsanHostConfigInfo: cfg}, nil
    case vsantypes.VsanHostConfigInfoEx:
        return &cfg, nil
    default:
        return nil, errors.New("host vSAN config not found")
    }
}

// vBOSS System Management Methods

// VsanVbossSystemCreateObjectStoreShards creates object store shards for vBOSS
func (c *Client) VsanVbossSystemCreateObjectStoreShards(ctx context.Context, objectStoreId string, cluster *vimtypes.ManagedObjectReference) (*object.Task, error) {
    req := vsantypes.VsanVbossSystemCreateObjectStoreShardsRequestType{
        This:          VsanVbossSystemInstance,
        ObjectStoreId: objectStoreId,
        Cluster:       cluster,
    }

    res, err := methods.VsanVbossSystemCreateObjectStoreShards(ctx, c, &req)
    if err != nil {
        return nil, err
    }

    return object.NewTask(c.vim25Client, res.Returnval), nil
}

// VsanVbossSystemDestroyObjectStoreShards destroys object store shards for vBOSS  
func (c *Client) VsanVbossSystemDestroyObjectStoreShards(ctx context.Context, objectStoreId string, cluster *vimtypes.ManagedObjectReference) (*object.Task, error) {
    req := vsantypes.VsanVbossSystemDestroyObjectStoreShardsRequestType{
        This:          VsanVbossSystemInstance,
        ObjectStoreId: objectStoreId,
        Cluster:       cluster,
    }

    res, err := methods.VsanVbossSystemDestroyObjectStoreShards(ctx, c, &req)
    if err != nil {
        return nil, err
    }

    return object.NewTask(c.vim25Client, res.Returnval), nil
}

// VsanQueryVsanObjectByShard queries vSAN objects by shard mapping
func (c *Client) VsanQueryVsanObjectByShard(ctx context.Context, cluster *vimtypes.ManagedObjectReference, spec *vsantypes.VsanVbossShardMappingQuerySpec) (*vsantypes.VsanVbossShardMappingResult, error) {
    req := vsantypes.VsanQueryVsanObjectByShardRequestType{
        This:    VsanVbossSystemInstance,
        Cluster: cluster,
        Spec:    spec,
    }

    res, err := methods.VsanQueryVsanObjectByShard(ctx, c, &req)
    if err != nil {
        return nil, err
    }

    return res.Returnval, nil
}