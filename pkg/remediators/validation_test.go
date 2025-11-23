package remediators

import (
	"testing"
)

// TestValidateDiskCleanupPath tests path validation for disk cleanup operations.
func TestValidateDiskCleanupPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid /tmp path",
			path:    "/tmp",
			wantErr: false,
		},
		{
			name:    "valid /var/tmp path",
			path:    "/var/tmp",
			wantErr: false,
		},
		{
			name:    "path traversal attempt with ..",
			path:    "/tmp/../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path traversal attempt with multiple ..",
			path:    "/tmp/../../etc",
			wantErr: true,
		},
		{
			name:    "unauthorized path /home",
			path:    "/home",
			wantErr: true,
		},
		{
			name:    "unauthorized path /etc",
			path:    "/etc",
			wantErr: true,
		},
		{
			name:    "unauthorized path /var/log",
			path:    "/var/log",
			wantErr: true,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
		{
			name:    "relative path ./tmp",
			path:    "./tmp",
			wantErr: true,
		},
		{
			name:    "path with trailing slash",
			path:    "/tmp/",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDiskCleanupPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDiskCleanupPath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestValidateInterfaceName tests interface name validation.
func TestValidateInterfaceName(t *testing.T) {
	tests := []struct {
		name    string
		iface   string
		wantErr bool
	}{
		{
			name:    "valid interface eth0",
			iface:   "eth0",
			wantErr: false,
		},
		{
			name:    "valid interface ens3",
			iface:   "ens3",
			wantErr: false,
		},
		{
			name:    "valid interface wlan0",
			iface:   "wlan0",
			wantErr: false,
		},
		{
			name:    "valid interface docker0",
			iface:   "docker0",
			wantErr: false,
		},
		{
			name:    "valid interface br-abc123",
			iface:   "br-abc123",
			wantErr: false,
		},
		{
			name:    "valid interface veth1234",
			iface:   "veth1234",
			wantErr: false,
		},
		{
			name:    "valid interface lo",
			iface:   "lo",
			wantErr: false,
		},
		{
			name:    "command injection with semicolon",
			iface:   "eth0; rm -rf /",
			wantErr: true,
		},
		{
			name:    "command injection with pipe",
			iface:   "eth0 | cat /etc/passwd",
			wantErr: true,
		},
		{
			name:    "command injection with ampersand",
			iface:   "eth0 && echo hacked",
			wantErr: true,
		},
		{
			name:    "command injection with backticks",
			iface:   "eth0`whoami`",
			wantErr: true,
		},
		{
			name:    "command injection with dollar sign",
			iface:   "eth0$(whoami)",
			wantErr: true,
		},
		{
			name:    "path traversal attempt",
			iface:   "../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path separator forward slash",
			iface:   "eth0/bad",
			wantErr: true,
		},
		{
			name:    "path separator backslash",
			iface:   "eth0\\bad",
			wantErr: true,
		},
		{
			name:    "empty interface name",
			iface:   "",
			wantErr: true,
		},
		{
			name:    "interface name too long (>15 chars)",
			iface:   "verylonginterfacename",
			wantErr: true,
		},
		{
			name:    "interface name with spaces",
			iface:   "eth 0",
			wantErr: true,
		},
		{
			name:    "interface name with special chars",
			iface:   "eth@0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInterfaceName(tt.iface)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInterfaceName(%q) error = %v, wantErr %v", tt.iface, err, tt.wantErr)
			}
		})
	}
}

// TestValidateVacuumSize tests journalctl vacuum size validation.
func TestValidateVacuumSize(t *testing.T) {
	tests := []struct {
		name    string
		size    string
		wantErr bool
	}{
		{
			name:    "valid size 500M",
			size:    "500M",
			wantErr: false,
		},
		{
			name:    "valid size 1G",
			size:    "1G",
			wantErr: false,
		},
		{
			name:    "valid size 100K",
			size:    "100K",
			wantErr: false,
		},
		{
			name:    "valid size 2048M",
			size:    "2048M",
			wantErr: false,
		},
		{
			name:    "invalid size 0M",
			size:    "0M",
			wantErr: true,
		},
		{
			name:    "invalid size without number",
			size:    "M",
			wantErr: true,
		},
		{
			name:    "invalid size without suffix",
			size:    "500",
			wantErr: true,
		},
		{
			name:    "invalid size with decimal",
			size:    "1.5G",
			wantErr: true,
		},
		{
			name:    "invalid size with lowercase suffix",
			size:    "500m",
			wantErr: true,
		},
		{
			name:    "invalid size with negative number",
			size:    "-100M",
			wantErr: true,
		},
		{
			name:    "empty size",
			size:    "",
			wantErr: true,
		},
		{
			name:    "invalid size with space",
			size:    "500 M",
			wantErr: true,
		},
		{
			name:    "command injection attempt",
			size:    "500M; rm -rf /",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVacuumSize(tt.size)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateVacuumSize(%q) error = %v, wantErr %v", tt.size, err, tt.wantErr)
			}
		})
	}
}

// TestValidateTmpFileAge tests tmp file age validation.
func TestValidateTmpFileAge(t *testing.T) {
	tests := []struct {
		name    string
		age     int
		wantErr bool
	}{
		{
			name:    "valid age 7 days",
			age:     7,
			wantErr: false,
		},
		{
			name:    "valid age 0 days",
			age:     0,
			wantErr: false,
		},
		{
			name:    "valid age 30 days",
			age:     30,
			wantErr: false,
		},
		{
			name:    "valid age 365 days",
			age:     365,
			wantErr: false,
		},
		{
			name:    "valid age 1000 days (suspicious but allowed)",
			age:     1000,
			wantErr: false,
		},
		{
			name:    "invalid negative age",
			age:     -1,
			wantErr: true,
		},
		{
			name:    "invalid very negative age",
			age:     -100,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTmpFileAge(tt.age)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTmpFileAge(%d) error = %v, wantErr %v", tt.age, err, tt.wantErr)
			}
		})
	}
}

// TestValidateDiskOperation tests disk operation validation.
func TestValidateDiskOperation(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		config    *DiskConfig
		wantErr   bool
	}{
		{
			name:      "valid clean-tmp operation",
			operation: "clean-tmp",
			config: &DiskConfig{
				Operation:  DiskCleanTmp,
				TmpFileAge: 7,
			},
			wantErr: false,
		},
		{
			name:      "valid clean-journal-logs operation",
			operation: "clean-journal-logs",
			config: &DiskConfig{
				Operation:         DiskCleanJournalLogs,
				JournalVacuumSize: "500M",
			},
			wantErr: false,
		},
		{
			name:      "valid clean-docker-images operation",
			operation: "clean-docker-images",
			config: &DiskConfig{
				Operation: DiskCleanDockerImages,
			},
			wantErr: false,
		},
		{
			name:      "valid clean-container-layers operation",
			operation: "clean-container-layers",
			config: &DiskConfig{
				Operation: DiskCleanContainerLayers,
			},
			wantErr: false,
		},
		{
			name:      "invalid clean-tmp with negative age",
			operation: "clean-tmp",
			config: &DiskConfig{
				Operation:  DiskCleanTmp,
				TmpFileAge: -5,
			},
			wantErr: true,
		},
		{
			name:      "invalid clean-journal-logs with bad size",
			operation: "clean-journal-logs",
			config: &DiskConfig{
				Operation:         DiskCleanJournalLogs,
				JournalVacuumSize: "invalid",
			},
			wantErr: true,
		},
		{
			name:      "unknown operation",
			operation: "unknown-operation",
			config:    &DiskConfig{},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDiskOperation(tt.operation, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDiskOperation(%q) error = %v, wantErr %v", tt.operation, err, tt.wantErr)
			}
		})
	}
}

// TestValidateNetworkOperation tests network operation validation.
func TestValidateNetworkOperation(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		config    *NetworkConfig
		wantErr   bool
	}{
		{
			name:      "valid restart-interface operation",
			operation: "restart-interface",
			config: &NetworkConfig{
				Operation:     NetworkRestartInterface,
				InterfaceName: "eth0",
			},
			wantErr: false,
		},
		{
			name:      "valid flush-dns operation",
			operation: "flush-dns",
			config: &NetworkConfig{
				Operation: NetworkFlushDNS,
			},
			wantErr: false,
		},
		{
			name:      "valid reset-routing operation",
			operation: "reset-routing",
			config: &NetworkConfig{
				Operation: NetworkResetRouting,
			},
			wantErr: false,
		},
		{
			name:      "invalid restart-interface with malicious name",
			operation: "restart-interface",
			config: &NetworkConfig{
				Operation:     NetworkRestartInterface,
				InterfaceName: "eth0; rm -rf /",
			},
			wantErr: true,
		},
		{
			name:      "invalid restart-interface with empty name",
			operation: "restart-interface",
			config: &NetworkConfig{
				Operation:     NetworkRestartInterface,
				InterfaceName: "",
			},
			wantErr: true,
		},
		{
			name:      "unknown operation",
			operation: "unknown-operation",
			config:    &NetworkConfig{},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNetworkOperation(tt.operation, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateNetworkOperation(%q) error = %v, wantErr %v", tt.operation, err, tt.wantErr)
			}
		})
	}
}
