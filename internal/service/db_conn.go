package service

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/dbridge/dbridge/internal/repository"
	cryptoPkg "github.com/dbridge/dbridge/pkg/crypto"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/sijms/go-ora/v2"
	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"
)

func isSupportedDBType(dbType string) bool {
	switch dbType {
	case "mysql", "mariadb", "oceanbase", "postgres", "postgresql", "oracle", "sqlserver", "sqlite":
		return true
	}
	return false
}

// openDBConn returns a standalone database/sql connection.
// Callers are responsible for closing it. For pooled connections, use ConnectDriver.
func openDBConn(ds repository.DataSource, pwd string) (*sql.DB, error) {
	var dsn string
	driverName := ds.Type

	switch ds.Type {
	case "mysql", "mariadb", "oceanbase":
		driverName = "mysql"
		if ds.Database != "" {
			dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=10s&readTimeout=30s",
				ds.Username, pwd, ds.Host, ds.Port, ds.Database)
		} else {
			dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/?timeout=10s&readTimeout=30s",
				ds.Username, pwd, ds.Host, ds.Port)
		}
	case "postgres", "postgresql":
		driverName = "postgres"
		dbName := ds.Database
		if dbName == "" {
			dbName = "postgres"
		}
		dsn = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable connect_timeout=10",
			ds.Host, ds.Port, ds.Username, pwd, dbName)
	case "oracle":
		driverName = "oracle"
		service := ds.Database
		if service == "" {
			service = "ORCLPDB1"
		}
		connectMode := "service_name"
		if ds.ExtraConfig != "" {
			var extra map[string]string
			if err := json.Unmarshal([]byte(ds.ExtraConfig), &extra); err == nil {
				if m, ok := extra["connect_mode"]; ok {
					connectMode = m
				}
				if s, ok := extra["oracle_service"]; ok && s != "" {
					service = s
				}
			}
		}
		if connectMode == "sid" {
			dsn = fmt.Sprintf("oracle://%s:%s@%s:%d/%s?SID=%s",
				ds.Username, pwd, ds.Host, ds.Port, service, service)
		} else {
			dsn = fmt.Sprintf("oracle://%s:%s@%s:%d/%s",
				ds.Username, pwd, ds.Host, ds.Port, service)
		}
	case "sqlserver":
		driverName = "sqlserver"
		dsn = fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable&connection+timeout=10",
			ds.Username, pwd, ds.Host, ds.Port, ds.Database)
	case "sqlite":
		driverName = "sqlite"
		dsn = fmt.Sprintf("file:%s?mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", ds.Host)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", ds.Type)
	}

	conn, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	if driverName == "sqlite" {
		conn.SetMaxOpenConns(1)
		conn.SetMaxIdleConns(1)
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	return conn, nil
}

// decryptDS decrypts the password for a data source
func decryptDS(ds *repository.DataSource) (string, error) {
	return cryptoPkg.Decrypt(ds.Password)
}
