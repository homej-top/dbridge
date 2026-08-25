package drivers

import "strings"

type CapabilitySet struct {
	DBType       string `json:"db_type"`
	Version      string `json:"version"`
	MajorVersion int    `json:"major_version"`
	HasRoles     bool   `json:"has_roles"`
	HasDeny      bool   `json:"has_deny"`
}

func DetectCapability(dbType, version string) *CapabilitySet {
	cs := &CapabilitySet{DBType: dbType, Version: version}
	switch dbType {
	case "mysql":
		cs.HasRoles = strings.Contains(version, "8.") || strings.Contains(version, "9.")
	case "oracle":
		cs.HasRoles = true
	case "postgres":
		cs.HasRoles = true
	case "sqlserver", "mssql":
		cs.HasDeny = true
		cs.HasRoles = true
	}
	return cs
}
