package hyperv

// MemoryOptions 描述虚拟机的内存配置，单位 MB。
type MemoryOptions struct {
	StartupMB int64 `json:"startupMB"`
	Dynamic   bool  `json:"dynamic"`
	MinimumMB int64 `json:"minimumMB"`
	MaximumMB int64 `json:"maximumMB"`
}

// NetworkAdapter 是虚拟机网络适配器的只读视图。
type NetworkAdapter struct {
	Name        string   `json:"name"`
	SwitchName  string   `json:"switchName"`
	MacAddress  string   `json:"macAddress"`
	Status      string   `json:"status"`
	IPAddresses []string `json:"ipAddresses"`
}

// HardDrive 是虚拟硬盘的只读视图。
type HardDrive struct {
	Path               string `json:"path"`
	ControllerType     string `json:"controllerType"`
	ControllerNumber   int    `json:"controllerNumber"`
	ControllerLocation int    `json:"controllerLocation"`
}

// DVDDrive 是虚拟光驱的只读视图。
type DVDDrive struct {
	Path               string `json:"path"`
	ControllerNumber   int    `json:"controllerNumber"`
	ControllerLocation int    `json:"controllerLocation"`
}

// VM 是虚拟机列表 / 详情使用的视图模型。
type VM struct {
	Name                 string           `json:"name"`
	VMID                 string           `json:"vmId"`
	State                string           `json:"state"`
	Generation           int              `json:"generation"`
	ProcessorCount       int              `json:"processorCount"`
	MemoryStartupMB      int64            `json:"memoryStartupMB"`
	MemoryAssignedMB     int64            `json:"memoryAssignedMB"`
	DynamicMemoryEnabled bool             `json:"dynamicMemoryEnabled"`
	MemoryMinimumMB      int64            `json:"memoryMinimumMB"`
	MemoryMaximumMB      int64            `json:"memoryMaximumMB"`
	UptimeSeconds        int64            `json:"uptimeSeconds"`
	Path                 string           `json:"path"`
	Version              string           `json:"version"`
	CheckpointType       string           `json:"checkpointType"`
	Status               string           `json:"status"`
	Networks             []NetworkAdapter `json:"networks"`
	HardDrives           []HardDrive      `json:"hardDrives"`
	DVDDrives            []DVDDrive       `json:"dvdDrives"`
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

// HostInfo 描述 Hyper-V 主机与增强会话状态。
type HostInfo struct {
	ComputerName             string `json:"computerName"`
	UserName                 string `json:"userName"`
	OSName                   string `json:"osName"`
	HyperVVersion            string `json:"hyperVVersion"`
	LogicalProcessors        int    `json:"logicalProcessors"`
	TotalMemoryMB            int64  `json:"totalMemoryMB"`
	VirtualMachinePath       string `json:"virtualMachinePath"`
	VirtualHardDiskPath      string `json:"virtualHardDiskPath"`
	InstallationType         string `json:"installationType"`
	EnhancedSessionSupported bool   `json:"enhancedSessionSupported"`
	EnhancedSessionEnabled   bool   `json:"enhancedSessionEnabled"`
	VMServiceStatus          string `json:"vmServiceStatus"`
	TermServiceStatus        string `json:"termServiceStatus"`
	IsAdmin                  bool   `json:"isAdmin"`
	ModuleAvailable          bool   `json:"moduleAvailable"`
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
	SourceName          string        `json:"sourceName"`
	NewName             string        `json:"newName"`
	TargetDirectory     string        `json:"targetDirectory"`
	StopSourceIfRunning bool          `json:"stopSourceIfRunning"`
	Memory              MemoryOptions `json:"memory"`
	CPUCount            int           `json:"cpuCount"`
	SwitchName          string        `json:"switchName"`
	BandwidthMbps       int           `json:"bandwidthMbps"`
}

// CreateRequest 对应新建虚拟机向导的完整参数。
type CreateRequest struct {
	Name               string        `json:"name"`
	Generation         int           `json:"generation"`
	Directory          string        `json:"directory"`
	Memory             MemoryOptions `json:"memory"`
	CPUCount           int           `json:"cpuCount"`
	SwitchName         string        `json:"switchName"`
	BandwidthMbps      int           `json:"bandwidthMbps"`
	DiskMode           string        `json:"diskMode"` // differencing | copy | new
	ImagePath          string        `json:"imagePath"`
	DiskSizeGB         int           `json:"diskSizeGB"`
	ISOPath            string        `json:"isoPath"`
	DisableCheckpoints bool          `json:"disableCheckpoints"`
}

// EditRequest 只携带需要修改的字段，nil 表示保持原值。
type EditRequest struct {
	NewName            string         `json:"newName,omitempty"`
	Memory             *MemoryOptions `json:"memory,omitempty"`
	CPUCount           *int           `json:"cpuCount,omitempty"`
	SwitchName         *string        `json:"switchName,omitempty"`
	BandwidthMbps      *int           `json:"bandwidthMbps,omitempty"`
	CheckpointsEnabled *bool          `json:"checkpointsEnabled,omitempty"`
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
