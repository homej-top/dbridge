package drivers

import "fmt"

// ─── MSSQL stub methods for non-SQLServer drivers ─────────────────────────
// These methods exist only to satisfy the DatabaseDriver interface.
// Only SQLServerDriver has real implementations.

func mssqlNotSupported(method string) error {
	return fmt.Errorf("%s: not supported for this database type", method)
}

// PostgreSQL stubs
func (d *PostgresDriver) ListLogins() ([]MSSQLLogin, error)                      { return nil, mssqlNotSupported("ListLogins") }
func (d *PostgresDriver) CreateLogin(req CreateLoginRequest) error                { return mssqlNotSupported("CreateLogin") }
func (d *PostgresDriver) DropLogin(loginName string, cascadeUsers bool) (*DropLoginResult, error) { return nil, mssqlNotSupported("DropLogin") }
func (d *PostgresDriver) AlterLogin(loginName string, req AlterLoginRequest) error { return mssqlNotSupported("AlterLogin") }
func (d *PostgresDriver) GetLoginDetail(loginName string) (*LoginDetail, error)   { return nil, mssqlNotSupported("GetLoginDetail") }
func (d *PostgresDriver) ListDatabaseUsers(database string) ([]MSSQLDatabaseUser, error) { return nil, mssqlNotSupported("ListDatabaseUsers") }
func (d *PostgresDriver) CreateDatabaseUser(database string, req CreateDBUserRequest) error { return mssqlNotSupported("CreateDatabaseUser") }
func (d *PostgresDriver) DropDatabaseUser(database, userName string) error        { return mssqlNotSupported("DropDatabaseUser") }
func (d *PostgresDriver) BatchCreateDatabaseUsers(loginName string, mappings []DBUserMapping) error { return mssqlNotSupported("BatchCreateDatabaseUsers") }
func (d *PostgresDriver) DetectOrphanedUsers(database string) ([]OrphanedUser, error) { return nil, mssqlNotSupported("DetectOrphanedUsers") }
func (d *PostgresDriver) FixOrphanedUser(database, userName, loginName string) error { return mssqlNotSupported("FixOrphanedUser") }
func (d *PostgresDriver) GetEffectivePermissions(database, principalName, objectType, objectName string) (*EffectivePermission, error) { return nil, mssqlNotSupported("GetEffectivePermissions") }
func (d *PostgresDriver) CheckGuestStatus(database string) (*GuestStatus, error)  { return nil, mssqlNotSupported("CheckGuestStatus") }
func (d *PostgresDriver) DisableGuest(database string) error                      { return mssqlNotSupported("DisableGuest") }

// MySQL stubs
func (d *MySQLDriver) ListLogins() ([]MSSQLLogin, error)                      { return nil, mssqlNotSupported("ListLogins") }
func (d *MySQLDriver) CreateLogin(req CreateLoginRequest) error                { return mssqlNotSupported("CreateLogin") }
func (d *MySQLDriver) DropLogin(loginName string, cascadeUsers bool) (*DropLoginResult, error) { return nil, mssqlNotSupported("DropLogin") }
func (d *MySQLDriver) AlterLogin(loginName string, req AlterLoginRequest) error { return mssqlNotSupported("AlterLogin") }
func (d *MySQLDriver) GetLoginDetail(loginName string) (*LoginDetail, error)   { return nil, mssqlNotSupported("GetLoginDetail") }
func (d *MySQLDriver) ListDatabaseUsers(database string) ([]MSSQLDatabaseUser, error) { return nil, mssqlNotSupported("ListDatabaseUsers") }
func (d *MySQLDriver) CreateDatabaseUser(database string, req CreateDBUserRequest) error { return mssqlNotSupported("CreateDatabaseUser") }
func (d *MySQLDriver) DropDatabaseUser(database, userName string) error        { return mssqlNotSupported("DropDatabaseUser") }
func (d *MySQLDriver) BatchCreateDatabaseUsers(loginName string, mappings []DBUserMapping) error { return mssqlNotSupported("BatchCreateDatabaseUsers") }
func (d *MySQLDriver) DetectOrphanedUsers(database string) ([]OrphanedUser, error) { return nil, mssqlNotSupported("DetectOrphanedUsers") }
func (d *MySQLDriver) FixOrphanedUser(database, userName, loginName string) error { return mssqlNotSupported("FixOrphanedUser") }
func (d *MySQLDriver) GetEffectivePermissions(database, principalName, objectType, objectName string) (*EffectivePermission, error) { return nil, mssqlNotSupported("GetEffectivePermissions") }
func (d *MySQLDriver) CheckGuestStatus(database string) (*GuestStatus, error)  { return nil, mssqlNotSupported("CheckGuestStatus") }
func (d *MySQLDriver) DisableGuest(database string) error                      { return mssqlNotSupported("DisableGuest") }

// Oracle stubs
func (d *OracleDriver) ListLogins() ([]MSSQLLogin, error)                      { return nil, mssqlNotSupported("ListLogins") }
func (d *OracleDriver) CreateLogin(req CreateLoginRequest) error                { return mssqlNotSupported("CreateLogin") }
func (d *OracleDriver) DropLogin(loginName string, cascadeUsers bool) (*DropLoginResult, error) { return nil, mssqlNotSupported("DropLogin") }
func (d *OracleDriver) AlterLogin(loginName string, req AlterLoginRequest) error { return mssqlNotSupported("AlterLogin") }
func (d *OracleDriver) GetLoginDetail(loginName string) (*LoginDetail, error)   { return nil, mssqlNotSupported("GetLoginDetail") }
func (d *OracleDriver) ListDatabaseUsers(database string) ([]MSSQLDatabaseUser, error) { return nil, mssqlNotSupported("ListDatabaseUsers") }
func (d *OracleDriver) CreateDatabaseUser(database string, req CreateDBUserRequest) error { return mssqlNotSupported("CreateDatabaseUser") }
func (d *OracleDriver) DropDatabaseUser(database, userName string) error        { return mssqlNotSupported("DropDatabaseUser") }
func (d *OracleDriver) BatchCreateDatabaseUsers(loginName string, mappings []DBUserMapping) error { return mssqlNotSupported("BatchCreateDatabaseUsers") }
func (d *OracleDriver) DetectOrphanedUsers(database string) ([]OrphanedUser, error) { return nil, mssqlNotSupported("DetectOrphanedUsers") }
func (d *OracleDriver) FixOrphanedUser(database, userName, loginName string) error { return mssqlNotSupported("FixOrphanedUser") }
func (d *OracleDriver) GetEffectivePermissions(database, principalName, objectType, objectName string) (*EffectivePermission, error) { return nil, mssqlNotSupported("GetEffectivePermissions") }
func (d *OracleDriver) CheckGuestStatus(database string) (*GuestStatus, error)  { return nil, mssqlNotSupported("CheckGuestStatus") }
func (d *OracleDriver) DisableGuest(database string) error                      { return mssqlNotSupported("DisableGuest") }
