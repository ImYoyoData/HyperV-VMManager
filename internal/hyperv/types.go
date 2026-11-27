package hyperv

// MemoryOptions 描述虚拟机的内存配置，单位 MB。
type MemoryOptions struct {
	StartupMB int64 `json:"startupMB"`
	Dynamic   bool  `json:"dynamic"`
	MinimumMB int64 `json:"minimumMB"`
	MaximumMB int64 `json:"maximumMB"`
}

// NetworkAdapter 是虚拟机网络适配器的视图与可编辑配置。
type NetworkAdapter struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	SwitchName          string   `json:"switchName"`
	MacAddress          string   `json:"macAddress"`
	DynamicMacAddress   bool     `json:"dynamicMacAddress"`
	MacAddressSpoofing  string   `json:"macAddressSpoofing"`
	DhcpGuard           string   `json:"dhcpGuard"`
	RouterGuard         string   `json:"routerGuard"`
	VlanMode            string   `json:"vlanMode"`
	VlanID              int      `json:"vlanID"`
	DeviceNaming        string   `json:"deviceNaming"`
	VmqWeight           int      `json:"vmqWeight"`
	IovWeight           int      `json:"iovWeight"`
	IPsecOffload        bool     `json:"ipsecOffload"`
	StormLimit          int      `json:"stormLimit"`
	MaximumBandwidthBps int64    `json:"maximumBandwidthBps"`
	Status              string   `json:"status"`
	IPAddresses         []string `json:"ipAddresses"`
}

// HardDrive 是虚拟硬盘的只读视图。
type HardDrive struct {
	Path               string `json:"path"`
	ControllerType     string `json:"controllerType"`
	ControllerNumber   int    `json:"controllerNumber"`
	ControllerLocation int    `json:"controllerLocation"`
	VirtualSizeBytes   int64  `json:"virtualSizeBytes"`
	FileSizeBytes      int64  `json:"fileSizeBytes"`
	MinimumSizeBytes   int64  `json:"minimumSizeBytes"`
	VHDType            string `json:"vhdType"`
	ParentPath         string `json:"parentPath"`
}

// DiskResize 是编辑虚拟机时对单块虚拟硬盘的容量调整。
type DiskResize struct {
	Path        string `json:"path"`
	SizeBytes   int64  `json:"sizeBytes"`
	AllowShrink bool   `json:"allowShrink,omitempty"`
}

// DVDDrive 是虚拟光驱的只读视图。
type DVDDrive struct {
	Path               string `json:"path"`
	ControllerNumber   int    `json:"controllerNumber"`
	ControllerLocation int    `json:"controllerLocation"`
}

// IntegrationService 是虚拟机集成服务的一项。
type IntegrationService struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Enabled     bool   `json:"enabled"`
}

// VM 是虚拟机列表 / 详情使用的视图模型。
type VM struct {
	Name                 string               `json:"name"`
	VMID                 string               `json:"vmId"`
	State                string               `json:"state"`
	Generation           int                  `json:"generation"`
	ProcessorCount       int                  `json:"processorCount"`
	MemoryStartupMB      int64                `json:"memoryStartupMB"`
	MemoryAssignedMB     int64                `json:"memoryAssignedMB"`
	DynamicMemoryEnabled bool                 `json:"dynamicMemoryEnabled"`
	MemoryMinimumMB      int64                `json:"memoryMinimumMB"`
	MemoryMaximumMB      int64                `json:"memoryMaximumMB"`
	UptimeSeconds        int64                `json:"uptimeSeconds"`
	Path                 string               `json:"path"`
	Version              string               `json:"version"`
	CheckpointType       string               `json:"checkpointType"`
	Status               string               `json:"status"`
	Networks             []NetworkAdapter     `json:"networks"`
	HardDrives           []HardDrive          `json:"hardDrives"`
	DVDDrives            []DVDDrive           `json:"dvdDrives"`
	NestedVirtualization bool                 `json:"nestedVirtualization"`
	Notes                string               `json:"notes"`
	AutomaticStartAction string               `json:"automaticStartAction"`
	AutomaticStartDelay  int                  `json:"automaticStartDelay"`
	AutomaticStopAction  string               `json:"automaticStopAction"`
	AutomaticCheckpoints bool                 `json:"automaticCheckpoints"`
	ProcessorReserve     int                  `json:"processorReserve"`
	ProcessorMaximum     int                  `json:"processorMaximum"`
	ProcessorWeight      int                  `json:"processorWeight"`
	ProcessorMigration   bool                 `json:"processorMigration"`
	HwThreadsPerCore     int                  `json:"hwThreadsPerCore"`
	MemoryBuffer         int                  `json:"memoryBuffer"`
	MemoryPriority       int                  `json:"memoryPriority"`
	SecureBoot           string               `json:"secureBoot"`
	SecureBootTemplate   string               `json:"secureBootTemplate"`
	NetworkBootProtocol  string               `json:"networkBootProtocol"`
	TpmEnabled           bool                 `json:"tpmEnabled"`
	Shielded             bool                 `json:"shielded"`
	EncryptTraffic       bool                 `json:"encryptTraffic"`
	VbsOptOut            bool                 `json:"vbsOptOut"`
	IntegrationServices  []IntegrationService `json:"integrationServices"`
}

// VirtualSwitch 是虚拟交换机的只读视图。
type VirtualSwitch struct {
	Name                           string `json:"name"`
	ID                             string `json:"id"`
	SwitchType                     string `json:"switchType"`
	NetAdapterInterfaceDescription string `json:"netAdapterInterfaceDescription"`
	AllowManagementOS              bool   `json:"allowManagementOS"`
	Status                         string `json:"status"`
}

// HostNetworkAdapter 是可用于创建外部交换机的物理网卡。
type HostNetworkAdapter struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	MacAddress  string `json:"macAddress"`
	LinkSpeed   string `json:"linkSpeed"`
	Status      string `json:"status"`
}

// NATStaticMapping 是与内部 NAT 网络关联的一条 Windows portproxy 端口转发规则。
type NATStaticMapping struct {
	Protocol          string `json:"protocol"` // 目前仅支持 TCP
	ExternalIPAddress string `json:"externalIPAddress"`
	ExternalPort      int    `json:"externalPort"`
	InternalIPAddress string `json:"internalIPAddress"`
	InternalPort      int    `json:"internalPort"`
}

// NATNetwork 是 Windows NAT 实例及其对应的内部交换机和主机地址。
type NATNetwork struct {
	Name           string             `json:"name"`
	Prefix         string             `json:"prefix"` // 192.168.100.0/24
	HostIP         string             `json:"hostIP"` // 192.168.100.1
	ExternalIP     string             `json:"externalIP"`
	SwitchName     string             `json:"switchName"`
	Active         bool               `json:"active"`
	StaticMappings []NATStaticMapping `json:"staticMappings"`
}

// CreateNATRequest 是创建“内部交换机 + 自建 NAT”的参数。
type CreateNATRequest struct {
	Name         string `json:"name"`
	SwitchName   string `json:"switchName"`
	HostIP       string `json:"hostIP"`
	PrefixLength int    `json:"prefixLength"`
}

// NATStaticMappingRequest 是新增 NAT 端口转发的参数。
type NATStaticMappingRequest struct {
	NATName           string `json:"natName"`
	Protocol          string `json:"protocol"`
	ExternalPort      int    `json:"externalPort"`
	InternalIPAddress string `json:"internalIPAddress"`
	InternalPort      int    `json:"internalPort"`
}

// RemoveNATStaticMappingRequest 是删除 NAT 端口转发的参数。
type RemoveNATStaticMappingRequest struct {
	NATName      string `json:"natName"`
	Protocol     string `json:"protocol"`
	ExternalPort int    `json:"externalPort"`
}

// LocalUser 是主机上的本地账户，用于在远程 VMConnect 时选择正确的登录名。
// PasswordRequired 为 false 通常表示账户没有设置密码；Windows 默认禁止
// 空密码本地账户进行远程登录（包括远程 VMConnect）。
type LocalUser struct {
	Name             string `json:"name"`
	FullName         string `json:"fullName"`
	Enabled          bool   `json:"enabled"`
	PasswordRequired bool   `json:"passwordRequired"`
	PrincipalSource  string `json:"principalSource"`
}

// SetLocalUserPasswordRequest 用于通过 WinRM 修改远程主机本地账户密码。
type SetLocalUserPasswordRequest struct {
	UserName string `json:"userName"`
	Password string `json:"password"`
}

// HostInfo 描述 Hyper-V 主机与增强会话状态。
type HostInfo struct {
	ComputerName             string      `json:"computerName"`
	UserName                 string      `json:"userName"`
	OSName                   string      `json:"osName"`
	HyperVVersion            string      `json:"hyperVVersion"`
	LogicalProcessors        int         `json:"logicalProcessors"`
	TotalMemoryMB            int64       `json:"totalMemoryMB"`
	VirtualMachinePath       string      `json:"virtualMachinePath"`
	VirtualHardDiskPath      string      `json:"virtualHardDiskPath"`
	InstallationType         string      `json:"installationType"`
	EnhancedSessionSupported bool        `json:"enhancedSessionSupported"`
	EnhancedSessionEnabled   bool        `json:"enhancedSessionEnabled"`
	VMServiceStatus          string      `json:"vmServiceStatus"`
	TermServiceStatus        string      `json:"termServiceStatus"`
	IsAdmin                  bool        `json:"isAdmin"`
	ModuleAvailable          bool        `json:"moduleAvailable"`
	LocalUsers               []LocalUser `json:"localUsers"`
}

// GPUDevice 是主机上的显示适配器。
type GPUDevice struct {
	Name          string `json:"name"`
	DriverVersion string `json:"driverVersion"`
	Status        string `json:"status"`
}

// PartitionableGPU 是支持 Hyper-V GPU 分区的显卡。
// Paravirtual 为 true 时表示这是客户端 Windows 上的 GPU-PV（共享整块显卡，
// 不能按分区限制显存），而不是 Windows Server 2025 的硬件分区 GPU-P。
type PartitionableGPU struct {
	Name                 string  `json:"name"`
	Paravirtual          bool    `json:"paravirtual"`
	ValidPartitionCounts []int32 `json:"validPartitionCounts"`
	PartitionCount       int32   `json:"partitionCount"`
	TotalVRAMBytes       int64   `json:"totalVRAMBytes"`
	AvailableVRAMBytes   int64   `json:"availableVRAMBytes"`
}

// EnhancedSessionStatus 是增强会话的详细状态。
type EnhancedSessionStatus struct {
	InstallationType  string `json:"installationType"`
	Supported         bool   `json:"supported"`
	HostPolicyEnabled bool   `json:"hostPolicyEnabled"`
	RegistryValue     int    `json:"registryValue"`
	VMServiceStatus   string `json:"vmServiceStatus"`
	TermServiceStatus string `json:"termServiceStatus"`
}

// CloneRequest 对应克隆虚拟机向导的完整参数。
type CloneRequest struct {
	SourceName           string        `json:"sourceName"`
	NewName              string        `json:"newName"`
	TargetDirectory      string        `json:"targetDirectory"`
	StopSourceIfRunning  bool          `json:"stopSourceIfRunning"`
	DiskMode             string        `json:"diskMode"` // copy | differencing
	ExportDirectory      string        `json:"exportDirectory,omitempty"`
	NestedVirtualization bool          `json:"nestedVirtualization"`
	Memory               MemoryOptions `json:"memory"`
	CPUCount             int           `json:"cpuCount"`
	SwitchName           string        `json:"switchName"`
	BandwidthMbps        int           `json:"bandwidthMbps"`
}

// CreateRequest 对应新建虚拟机向导的完整参数。
type CreateRequest struct {
	Name                 string        `json:"name"`
	Generation           int           `json:"generation"`
	Directory            string        `json:"directory"`
	Memory               MemoryOptions `json:"memory"`
	CPUCount             int           `json:"cpuCount"`
	SwitchName           string        `json:"switchName"`
	BandwidthMbps        int           `json:"bandwidthMbps"`
	DiskMode             string        `json:"diskMode"` // differencing | copy | new
	DiskType             string        `json:"diskType"` // dynamic | fixed
	DiskDirectory        string        `json:"diskDirectory,omitempty"`
	ImagePath            string        `json:"imagePath"`
	DiskSizeGB           int           `json:"diskSizeGB"`
	ISOPath              string        `json:"isoPath"`
	DisableCheckpoints   bool          `json:"disableCheckpoints"`
	NestedVirtualization bool          `json:"nestedVirtualization"`
}

// NetworkAdapterUpdate 是编辑虚拟机时对单个网络适配器的修改。
type NetworkAdapterUpdate struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name,omitempty"`
	SwitchName         *string `json:"switchName,omitempty"`
	DynamicMacAddress  *bool   `json:"dynamicMacAddress,omitempty"`
	StaticMacAddress   *string `json:"staticMacAddress,omitempty"`
	MacAddressSpoofing *string `json:"macAddressSpoofing,omitempty"`
	DhcpGuard          *string `json:"dhcpGuard,omitempty"`
	RouterGuard        *string `json:"routerGuard,omitempty"`
	VlanMode           *string `json:"vlanMode,omitempty"`
	VlanID             *int    `json:"vlanID,omitempty"`
	DeviceNaming       *string `json:"deviceNaming,omitempty"`
	VmqWeight          *int    `json:"vmqWeight,omitempty"`
	IovWeight          *int    `json:"iovWeight,omitempty"`
	IPsecOffload       *bool   `json:"ipsecOffload,omitempty"`
	StormLimit         *int    `json:"stormLimit,omitempty"`
	BandwidthMbps      *int    `json:"bandwidthMbps,omitempty"`
}

// SecurityUpdate 是虚拟机安全与固件设置。
type SecurityUpdate struct {
	TpmEnabled                        *bool   `json:"tpmEnabled,omitempty"`
	EncryptStateAndVmMigrationTraffic *bool   `json:"encryptStateAndVmMigrationTraffic,omitempty"`
	VirtualizationBasedSecurityOptOut *bool   `json:"virtualizationBasedSecurityOptOut,omitempty"`
	SecureBoot                        *bool   `json:"secureBoot,omitempty"`
	SecureBootTemplate                *string `json:"secureBootTemplate,omitempty"`
	PreferredNetworkBootProtocol      *string `json:"preferredNetworkBootProtocol,omitempty"`
}

// IntegrationServiceUpdate 是单个集成服务的开关。
type IntegrationServiceUpdate struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// EditRequest 只携带需要修改的字段，nil 表示保持原值。
type EditRequest struct {
	NewName              string         `json:"newName,omitempty"`
	Memory               *MemoryOptions `json:"memory,omitempty"`
	CPUCount             *int           `json:"cpuCount,omitempty"`
	SwitchName           *string        `json:"switchName,omitempty"`
	BandwidthMbps        *int           `json:"bandwidthMbps,omitempty"`
	CheckpointsEnabled   *bool          `json:"checkpointsEnabled,omitempty"`
	NestedVirtualization *bool          `json:"nestedVirtualization,omitempty"`
	Disks                []DiskResize   `json:"disks,omitempty"`
	StopVMForDiskResize  bool           `json:"stopVMForDiskResize,omitempty"`

	Notes                *string                    `json:"notes,omitempty"`
	AutomaticStartAction *string                    `json:"automaticStartAction,omitempty"`
	AutomaticStartDelay  *int                       `json:"automaticStartDelay,omitempty"`
	AutomaticStopAction  *string                    `json:"automaticStopAction,omitempty"`
	AutomaticCheckpoints *bool                      `json:"automaticCheckpoints,omitempty"`
	ProcessorReserve     *int                       `json:"processorReserve,omitempty"`
	ProcessorMaximum     *int                       `json:"processorMaximum,omitempty"`
	ProcessorWeight      *int                       `json:"processorWeight,omitempty"`
	ProcessorMigration   *bool                      `json:"processorMigration,omitempty"`
	HwThreadsPerCore     *int                       `json:"hwThreadsPerCore,omitempty"`
	MemoryBuffer         *int                       `json:"memoryBuffer,omitempty"`
	MemoryPriority       *int                       `json:"memoryPriority,omitempty"`
	NetworkAdapters      []NetworkAdapterUpdate     `json:"networkAdapters,omitempty"`
	Security             *SecurityUpdate            `json:"security,omitempty"`
	IntegrationServices  []IntegrationServiceUpdate `json:"integrationServices,omitempty"`
}

// CreateSwitchRequest 用于创建虚拟交换机。
type CreateSwitchRequest struct {
	Name              string `json:"name"`
	SwitchType        string `json:"switchType"` // External | Internal | Private
	NetAdapterName    string `json:"netAdapterName"`
	AllowManagementOS bool   `json:"allowManagementOS"`
}

// PowerAction 是电源操作的动作名。
type PowerAction string

const (
	PowerStart   PowerAction = "start"
	PowerStop    PowerAction = "stop"    // 正常关机
	PowerTurnOff PowerAction = "turnoff" // 强制断电
	PowerSave    PowerAction = "save"
	PowerPause   PowerAction = "pause"
	PowerResume  PowerAction = "resume"
	PowerRestart PowerAction = "restart"
)

// ProgressFunc 由任务队列注入，用于上报进度与当前步骤。
type ProgressFunc func(percent int, step string)

// DiskSpace 描述一个逻辑磁盘的容量。
type DiskSpace struct {
	Drive      string `json:"drive"`
	TotalBytes int64  `json:"totalBytes"`
	FreeBytes  int64  `json:"freeBytes"`
}

// HostCapacity 汇总主机的 CPU / 内存 / 磁盘分配情况。
type HostCapacity struct {
	HostID            string      `json:"hostID"`
	Name              string      `json:"name"`
	IsLocal           bool        `json:"isLocal"`
	LogicalProcessors int         `json:"logicalProcessors"`
	AllocatedCPU      int         `json:"allocatedCPU"`
	AvailableCPU      int         `json:"availableCPU"`
	TotalMemoryMB     int64       `json:"totalMemoryMB"`
	AllocatedMemoryMB int64       `json:"allocatedMemoryMB"`
	ReservedMemoryMB  int64       `json:"reservedMemoryMB"`
	AvailableMemoryMB int64       `json:"availableMemoryMB"`
	Disks             []DiskSpace `json:"disks"`
}
