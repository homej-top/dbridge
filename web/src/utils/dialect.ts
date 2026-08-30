/**
 * DbDialect — unified interface for database-specific SQL generation.
 * Each database type has its own implementation to isolate syntax differences.
 * Usage: const d = getDialect(dbType); d.selectQuery(schema, table, cols);
 */

export type Dialect = 'mysql' | 'postgres' | 'oracle' | 'sqlserver';

export function dialectOf(dbType: string): Dialect {
  switch (dbType) {
    case 'mysql':
    case 'mariadb':
    case 'oceanbase':
      return 'mysql';
    case 'postgres':
    case 'postgresql':
      return 'postgres';
    case 'oracle':
      return 'oracle';
    case 'sqlserver':
      return 'sqlserver';
    default:
      return 'mysql';
  }
}

export interface ColumnDef {
  name: string;
  type: string;
  length?: string;
  nullable: boolean;
  defaultValue?: string;
  comment?: string;
}

export interface CreateTableParams {
  schema: string;
  table: string;
  columns: ColumnDef[];
  primaryKey?: string;
  autoIncrement?: boolean;
  comment?: string;
  engine?: string;
  charset?: string;
}

export interface DbDialect {
  // ─── Identifier quoting ─────────────────────────────────────────────────
  quoteIdent(name: string): string;
  qualifiedTable(schema: string, table: string): string;

  // ─── Value formatting ──────────────────────────────────────────────────
  quoteLiteral(value: string): string;
  /** Format a value for INSERT/UPDATE/SET; numeric types skip quoting */
  formatValue(value: string, columnType?: string): string;
  /** Check if a column type is numeric (INT, DECIMAL, NUMBER, etc.) */
  isNumericType(colType?: string): boolean;

  // ─── DML statements ────────────────────────────────────────────────────
  /** SELECT columns FROM schema.table [WHERE whereClause] [ORDER BY orderBy] */
  selectQuery(schema: string, table: string, columns?: string[], whereClause?: string, orderBy?: string): string;
  /** SELECT COUNT(*) as cnt FROM schema.table [WHERE whereClause] */
  countQuery(schema: string, table: string, whereClause?: string): string;
  /** DELETE FROM schema.table WHERE ... */
  deleteQuery(schema: string, table: string, whereClause: string): string;
  /** Full INSERT statement */
  insertQuery(schema: string, table: string, columns: string[], values: Record<string, any>, columnTypes?: Record<string, string>): string;
  /** Full UPDATE statement */
  updateQuery(schema: string, table: string, sets: Record<string, any>, where: Record<string, any>, columnTypes?: Record<string, string>): string;

  // ─── DDL statements ────────────────────────────────────────────────────
  /** DROP TABLE [IF EXISTS] schema.table */
  dropTable(schema: string, table: string, ifExists?: boolean): string;
  /** DROP VIEW [IF EXISTS] schema.view */
  dropView(schema: string, view: string, ifExists?: boolean): string;
  /** CREATE TABLE with columns, PK, comments. Returns full DDL including COMMENT ON statements. */
  createTableDDL(params: CreateTableParams): string;
  /** CREATE [OR REPLACE] VIEW schema.view AS selectSQL */
  createViewDDL(schema: string, view: string, selectSQL: string, orReplace?: boolean): string;

  // ─── Database / Schema management ──────────────────────────────────────
  /** CREATE DATABASE (MySQL: charset+collation; PG/SQL Server: simple) */
  createDatabase(name: string, charset?: string, collation?: string): string;
  /** DROP DATABASE [IF EXISTS] (CASCADE for Oracle user) */
  dropDatabase(name: string): string;
  /** CREATE SCHEMA */
  createSchema(schema: string): string;
  /** DROP SCHEMA */
  dropSchema(schema: string): string;
  /** CREATE USER (Oracle only) */
  createUser(name: string, password?: string): string;
  /** DROP USER (Oracle only, with CASCADE) */
  dropUser(name: string): string;

  // ─── Compatibility ─────────────────────────────────────────────────────
  /** Whether this database supports IF EXISTS in DROP statements */
  supportsIfExists(): boolean;
  processColumns(): { key: string; title: string; width: number }[];
  databaseColumns(): { key: string; title: string; width: number }[];
  userColumns(): { key: string; title: string; width: number }[];
  roleColumns(): { key: string; title: string; width: number }[];
  privilegeColumns(): { key: string; title: string; width: number }[];
  tablespaceColumns(): { key: string; title: string; width: number }[];
}

// ─── Helpers ────────────────────────────────────────────────────────────────

const numericRe = /^(int|bigint|smallint|tinyint|mediumint|integer|numeric|decimal|number|float|double|real|money|bit|serial|bigserial|smallserial)$/i;

function quoteMysql(name: string) { return '`' + name.replace(/`/g, '``') + '`'; }
function quotePg(name: string) { return '"' + name.replace(/"/g, '""') + '"'; }
function quoteOracleFn(n: string) { return '"' + n.replace(/"/g, '""') + '"'; }
function quoteMssql(name: string) { return '[' + name.replace(/]/g, ']]') + ']'; }
function quoteLiteral(v: string) { return "'" + v.replace(/'/g, "''") + "'"; }

/** Check if a column type typically requires a length parameter. */
function typeNeedsLength(colType: string): boolean {
  const t = colType.toLowerCase();
  return /^(varchar|nvarchar|char|nchar|varchar2|varbinary|binary|raw)$/i.test(t);
}

/** Format column type with optional length. E.g., VARCHAR(255) or INT. */
function formatColumnType(col: { type: string; length?: string }, defaultNeedsLen?: boolean): string {
  const hasLen = col.length && col.length !== '0';
  if (hasLen) return `${col.type}(${col.length})`;
  if (defaultNeedsLen && !hasLen && typeNeedsLength(col.type)) return `${col.type}(255)`;
  return col.type;
}

/** Quote a default value if it's a plain string (not number, function, keyword, or already quoted). */
function quoteDefaultValue(val: string): string {
  const trimmed = val.trim();
  if (!trimmed) return '';
  if (/^-?\d+(\.\d+)?$/.test(trimmed)) return trimmed;
  if ((trimmed.startsWith("'") && trimmed.endsWith("'")) || (trimmed.startsWith('"') && trimmed.endsWith('"'))) return trimmed;
  if (trimmed.includes('(')) return trimmed;
  if (/^(NULL|CURRENT_TIMESTAMP|GETDATE|NOW|SYSDATE|CURRENT_DATE|CURRENT_TIME|TRUE|FALSE)$/i.test(trimmed)) return trimmed;
  return "'" + trimmed.replace(/'/g, "''") + "'";
}

function makeSelectQuery(quote: (s: string) => string) {
  return (s: string, t: string, cols?: string[], where?: string, orderBy?: string) => {
    const c = cols?.length ? cols.map(c => quote(c)).join(', ') : '*';
    let sql = `SELECT ${c} FROM ${quote(s)}.${quote(t)}`;
    if (where) sql += ` WHERE ${where}`;
    if (orderBy) sql += ` ORDER BY ${orderBy}`;
    return sql;
  };
}
function makeCountQuery(quote: (s: string) => string) {
  return (s: string, t: string, where?: string) => {
    let sql = `SELECT COUNT(*) as cnt FROM ${quote(s)}.${quote(t)}`;
    if (where) sql += ` WHERE ${where}`;
    return sql;
  };
}

// ─── MySQL ─────────────────────────────────────────────────────────────────

const mysqlDialect: DbDialect = {
  quoteIdent: (n) => quoteMysql(n),
  quoteLiteral,
  qualifiedTable: (s, t) => `${quoteMysql(s)}.${quoteMysql(t)}`,
  isNumericType: (ct) => numericRe.test(ct || ''),
  formatValue: (v, ct) => {
    if (v === '' || v == null) return 'NULL';
    if (numericRe.test(ct || '')) return v;
    return quoteLiteral(v);
  },
  selectQuery: makeSelectQuery(quoteMysql),
  countQuery: makeCountQuery(quoteMysql),
  deleteQuery: (s, t, w) => `DELETE FROM ${quoteMysql(s)}.${quoteMysql(t)} WHERE ${w}`,
  insertQuery: (s, t, cols, vals, ct) => {
    const qCols = cols.map(c => quoteMysql(c)).join(', ');
    const qVals = cols.map(c => vals[c] === '' || vals[c] == null ? 'NULL' : (numericRe.test(ct?.[c] || '') ? String(vals[c]) : quoteLiteral(String(vals[c])))).join(', ');
    return `INSERT INTO ${quoteMysql(s)}.${quoteMysql(t)} (${qCols}) VALUES (${qVals})`;
  },
  updateQuery: (s, t, sets, where, ct) => {
    const setParts = Object.entries(sets).map(([k, v]) => {
      if (v === '' || v == null) return `${quoteMysql(k)} = NULL`;
      return `${quoteMysql(k)} = ${numericRe.test(ct?.[k] || '') ? v : quoteLiteral(String(v))}`;
    }).join(', ');
    const whereParts = Object.entries(where).map(([k, v]) => {
      return `${quoteMysql(k)} = ${numericRe.test(ct?.[k] || '') ? v : quoteLiteral(String(v))}`;
    }).join(' AND ');
    return `UPDATE ${quoteMysql(s)}.${quoteMysql(t)} SET ${setParts} WHERE ${whereParts}`;
  },
  dropTable: (s, t, ie) => `DROP TABLE${ie !== false ? ' IF EXISTS' : ''} ${quoteMysql(s)}.${quoteMysql(t)}`,
  dropView: (s, v, ie) => `DROP VIEW${ie !== false ? ' IF EXISTS' : ''} ${quoteMysql(s)}.${quoteMysql(v)}`,
  createViewDDL: (s, v, sql, orReplace) => orReplace !== false
    ? `CREATE OR REPLACE VIEW ${quoteMysql(s)}.${quoteMysql(v)} AS\n${sql}`
    : `CREATE VIEW ${quoteMysql(s)}.${quoteMysql(v)} AS\n${sql}`,
  createTableDDL: (p) => {
    const q = quoteMysql;
    const lines: string[] = [];
    for (const col of p.columns) {
      let def = `  ${q(col.name)} ${formatColumnType(col, false)}`;
      if (!col.nullable) def += ' NOT NULL';
      if (p.autoIncrement && col.name === p.primaryKey) def += ' AUTO_INCREMENT';
      if (col.defaultValue) def += ` DEFAULT ${col.defaultValue}`;
      if (col.comment) def += ` COMMENT '${col.comment.replace(/'/g, "''")}'`;
      lines.push(def);
    }
    if (p.primaryKey) lines.push(`  PRIMARY KEY (${q(p.primaryKey)})`);
    let opts = ` ENGINE=${p.engine || 'InnoDB'} DEFAULT CHARSET=${p.charset || 'utf8mb4'}`;
    if (p.comment) opts += ` COMMENT='${p.comment.replace(/'/g, "''")}'`;
    return `CREATE TABLE ${q(p.schema)}.${q(p.table)} (\n${lines.join(',\n')}\n)${opts};`;
  },
  createDatabase: (n, cs, col) => {
    const qn = quoteMysql(n);
    let sql = `CREATE DATABASE ${qn}`;
    if (cs) sql += `\n  CHARACTER SET ${cs}`;
    if (col) sql += `\n  COLLATE ${col}`;
    return sql + ';';
  },
  processColumns: () => [
    { dataIndex: 'pid', key: 'pid', title: 'PID', width: 80 }, { dataIndex: 'username', key: 'username', title: '用户', width: 100 },
    { dataIndex: 'host', key: 'host', title: '来源', width: 130 }, { dataIndex: 'database_name', key: 'database_name', title: '数据库', width: 100 },
    { dataIndex: 'state', key: 'state', title: '状态', width: 80 }, { dataIndex: 'seconds', key: 'seconds', title: '运行(s)', width: 80 },
    { dataIndex: 'query', key: 'query', title: 'SQL', width: 300 },
  ],
  databaseColumns: () => [
    { dataIndex: 'name', key: 'name', title: '数据库名称', width: 180 }, { dataIndex: 'charset', key: 'charset', title: '字符集', width: 120 },
    { dataIndex: 'size_mb', key: 'size_mb', title: '大小(MB)', width: 100 }, { dataIndex: 'tables', key: 'tables', title: '表数量', width: 80 },
  ],
  userColumns: () => [
    { dataIndex: 'username', key: 'username', title: '用户名', width: 150 }, { dataIndex: 'host', key: 'host', title: '主机', width: 150 },
  ],
  roleColumns: () => [
    { dataIndex: 'name', key: 'name', title: '角色名', width: 180 }, { dataIndex: 'members', key: 'members', title: '成员', width: 300 },
  ],
  privilegeColumns: () => [
    { dataIndex: 'database', key: 'database', title: '数据库', width: 120 }, { dataIndex: 'object_type', key: 'object_type', title: '类型', width: 80 },
    { dataIndex: 'object_name', key: 'object_name', title: '对象', width: 150 }, { dataIndex: 'privileges', key: 'privileges', title: '权限', width: 250 },
  ],
  tablespaceColumns: () => [],
  dropDatabase: (n) => `DROP DATABASE IF EXISTS ${quoteMysql(n)}`,
  createSchema: (s) => `CREATE SCHEMA ${quoteMysql(s)}`,
  dropSchema: (s) => `DROP SCHEMA IF EXISTS ${quoteMysql(s)}`,
  createUser: () => { throw new Error('Not supported on MySQL'); },
  dropUser: () => { throw new Error('Not supported on MySQL'); },
  supportsIfExists: () => true,
};

// ─── PostgreSQL ────────────────────────────────────────────────────────────

const pgDialect: DbDialect = {
  quoteIdent: (n) => quotePg(n),
  quoteLiteral,
  qualifiedTable: (s, t) => `${quotePg(s)}.${quotePg(t)}`,
  isNumericType: (ct) => numericRe.test(ct || ''),
  formatValue: (v, ct) => {
    if (v === '' || v == null) return 'NULL';
    if (numericRe.test(ct || '')) return v;
    return quoteLiteral(v);
  },
  selectQuery: makeSelectQuery(quotePg),
  countQuery: makeCountQuery(quotePg),
  deleteQuery: (s, t, w) => `DELETE FROM ${quotePg(s)}.${quotePg(t)} WHERE ${w}`,
  insertQuery: (s, t, cols, vals, ct) => {
    const qCols = cols.map(c => quotePg(c)).join(', ');
    const qVals = cols.map(c => vals[c] === '' || vals[c] == null ? 'NULL' : (numericRe.test(ct?.[c] || '') ? String(vals[c]) : quoteLiteral(String(vals[c])))).join(', ');
    return `INSERT INTO ${quotePg(s)}.${quotePg(t)} (${qCols}) VALUES (${qVals})`;
  },
  updateQuery: (s, t, sets, where, ct) => {
    const setParts = Object.entries(sets).map(([k, v]) => {
      if (v === '' || v == null) return `${quotePg(k)} = NULL`;
      return `${quotePg(k)} = ${numericRe.test(ct?.[k] || '') ? v : quoteLiteral(String(v))}`;
    }).join(', ');
    const whereParts = Object.entries(where).map(([k, v]) => {
      return `${quotePg(k)} = ${numericRe.test(ct?.[k] || '') ? v : quoteLiteral(String(v))}`;
    }).join(' AND ');
    return `UPDATE ${quotePg(s)}.${quotePg(t)} SET ${setParts} WHERE ${whereParts}`;
  },
  dropTable: (s, t, ie) => `DROP TABLE${ie !== false ? ' IF EXISTS' : ''} ${quotePg(s)}.${quotePg(t)}`,
  dropView: (s, v, ie) => `DROP VIEW${ie !== false ? ' IF EXISTS' : ''} ${quotePg(s)}.${quotePg(v)}`,
  createViewDDL: (s, v, sql, orReplace) => `CREATE${orReplace !== false ? ' OR REPLACE' : ''} VIEW ${quotePg(s)}.${quotePg(v)} AS\n${sql}`,
  createTableDDL: (p) => {
    const q = quotePg;
    const lines: string[] = [];
    for (const col of p.columns) {
      let def = `  ${q(col.name)} ${formatColumnType(col, true)}`;
      if (!col.nullable) def += ' NOT NULL';
      if (col.defaultValue) def += ` DEFAULT ${quoteDefaultValue(col.defaultValue)}`;
      lines.push(def);
    }
    if (p.primaryKey) lines.push(`  PRIMARY KEY (${q(p.primaryKey)})`);
    let ddl = `CREATE TABLE ${q(p.schema)}.${q(p.table)} (\n${lines.join(',\n')}\n);`;
    // Separate COMMENT ON statements
    const extras: string[] = [];
    if (p.comment) extras.push(`COMMENT ON TABLE ${q(p.schema)}.${q(p.table)} IS '${p.comment.replace(/'/g, "''")}';`);
    for (const col of p.columns) {
      if (col.comment) extras.push(`COMMENT ON COLUMN ${q(p.schema)}.${q(p.table)}.${q(col.name)} IS '${col.comment.replace(/'/g, "''")}';`);
    }
    if (extras.length) ddl += '\n' + extras.join('\n');
    return ddl;
  },
  createDatabase: (n) => `CREATE DATABASE ${quotePg(n)}`,
  processColumns: () => [
    { dataIndex: 'pid', key: 'pid', title: 'PID', width: 80 }, { dataIndex: 'username', key: 'username', title: '用户', width: 100 },
    { dataIndex: 'application_name', key: 'application_name', title: '应用', width: 120 }, { dataIndex: 'client_addr', key: 'client_addr', title: '来源', width: 130 },
    { dataIndex: 'state', key: 'state', title: '状态', width: 80 }, { dataIndex: 'seconds', key: 'seconds', title: '运行(s)', width: 80 },
    { dataIndex: 'query', key: 'query', title: 'SQL', width: 300 },
  ],
  databaseColumns: () => [
    { dataIndex: 'name', key: 'name', title: '数据库名称', width: 180 }, { dataIndex: 'charset', key: 'charset', title: '编码', width: 100 },
    { dataIndex: 'size_mb', key: 'size_mb', title: '大小(MB)', width: 100 }, { dataIndex: 'tables', key: 'tables', title: '表数量', width: 80 },
  ],
  userColumns: () => [
    { dataIndex: 'username', key: 'username', title: '用户名', width: 150 },
    { dataIndex: 'is_superuser', key: 'is_superuser', title: '超级用户', width: 80, render: (v: any) => v === 'true' || v === true ? '✅' : '' },
    { dataIndex: 'can_createdb', key: 'can_createdb', title: '建库', width: 60, render: (v: any) => v === 'true' || v === true ? '✅' : '' },
    { dataIndex: 'can_createrole', key: 'can_createrole', title: '建角色', width: 70, render: (v: any) => v === 'true' || v === true ? '✅' : '' },
    { dataIndex: 'can_replication', key: 'can_replication', title: '复制', width: 60, render: (v: any) => v === 'true' || v === true ? '✅' : '' },
    { dataIndex: 'valid_until', key: 'valid_until', title: '有效期', width: 120, render: (v: any) => v && v !== 'infinity' ? String(v).substring(0, 10) : '' },
  ],
  roleColumns: () => [
    { dataIndex: 'name', key: 'name', title: '角色名', width: 150 },
    { dataIndex: 'is_system', key: 'is_system', title: '系统', width: 50, render: (v: any) => v === true || v === 'true' ? '✅' : '' },
    { dataIndex: 'can_login', key: 'can_login', title: '可登录', width: 70, render: (v: any) => v === 'true' ? '✅' : '—' },
    { dataIndex: 'is_superuser', key: 'is_superuser', title: '超级用户', width: 80, render: (v: any) => v === 'true' ? '✅' : '' },
    { dataIndex: 'can_createdb', key: 'can_createdb', title: '建库', width: 50, render: (v: any) => v === 'true' ? '✅' : '' },
    { dataIndex: 'can_createrole', key: 'can_createrole', title: '建角色', width: 60, render: (v: any) => v === 'true' ? '✅' : '' },
    { dataIndex: 'can_inherit', key: 'can_inherit', title: '继承', width: 50, render: (v: any) => v === 'true' ? '✅' : '⚠️' },
    { dataIndex: 'members', key: 'members', title: '子角色数', width: 70, render: (v: any[]) => v?.length || 0 },
  ],
  privilegeColumns: () => [
    { dataIndex: 'database', key: 'database', title: 'Schema', width: 120 }, { dataIndex: 'object_type', key: 'object_type', title: '类型', width: 80 },
    { dataIndex: 'object_name', key: 'object_name', title: '对象', width: 150 }, { dataIndex: 'privileges', key: 'privileges', title: '权限', width: 250 },
  ],
  tablespaceColumns: () => [
    { dataIndex: 'name', key: 'name', title: '名称', width: 200 }, { dataIndex: 'size_mb', key: 'size_mb', title: '大小(MB)', width: 120 },
  ],
  dropDatabase: (n) => `DROP DATABASE IF EXISTS ${quotePg(n)}`,
  createSchema: (s) => `CREATE SCHEMA ${quotePg(s)}`,
  dropSchema: (s) => `DROP SCHEMA IF EXISTS ${quotePg(s)}`,
  createUser: () => { throw new Error('Not supported on PostgreSQL'); },
  dropUser: () => { throw new Error('Not supported on PostgreSQL'); },
  supportsIfExists: () => true,
};

// ─── SQLite ────────────────────────────────────────────────────────────────

const quoteSQLite = (n: string) => `"${n.replace(/"/g, '""')}"`;

const sqliteDialect: DbDialect = {
  quoteIdent: (n) => quoteSQLite(n),
  quoteLiteral,
  qualifiedTable: (_s, t) => quoteSQLite(t),
  isNumericType: (ct) => numericRe.test(ct || ''),
  formatValue: (v, ct) => {
    if (v === '' || v == null) return 'NULL';
    if (numericRe.test(ct || '')) return v;
    return quoteLiteral(v);
  },
  selectQuery: (_s, t, cols, where, orderBy) => {
    const c = cols?.length ? cols.map(quoteSQLite).join(', ') : '*';
    let sql = `SELECT ${c} FROM ${quoteSQLite(t)}`;
    if (where) sql += ` WHERE ${where}`;
    if (orderBy) sql += ` ORDER BY ${orderBy}`;
    return sql;
  },
  countQuery: (_s, t) => `SELECT COUNT(*) FROM ${quoteSQLite(t)}`,
  deleteQuery: (_s, t, w) => `DELETE FROM ${quoteSQLite(t)} WHERE ${w}`,
  insertQuery: (_s, t, cols, vals, ct) => {
    const qCols = cols.map(c => quoteSQLite(c)).join(', ');
    const qVals = cols.map(c => vals[c] === '' || vals[c] == null ? 'NULL' : (numericRe.test(ct?.[c] || '') ? String(vals[c]) : quoteLiteral(String(vals[c])))).join(', ');
    return `INSERT INTO ${quoteSQLite(t)} (${qCols}) VALUES (${qVals})`;
  },
  updateQuery: (_s, t, sets, where, ct) => {
    const setParts = Object.entries(sets).map(([k, v]) => {
      if (v === '' || v == null) return `${quoteSQLite(k)} = NULL`;
      return `${quoteSQLite(k)} = ${numericRe.test(ct?.[k] || '') ? v : quoteLiteral(String(v))}`;
    }).join(', ');
    const whereParts = Object.entries(where).map(([k, v]) => `${quoteSQLite(k)} = ${numericRe.test(ct?.[k] || '') ? v : quoteLiteral(String(v))}`).join(' AND ');
    return `UPDATE ${quoteSQLite(t)} SET ${setParts} WHERE ${whereParts}`;
  },
  dropTable: (_s, t, ie) => `DROP TABLE${ie !== false ? ' IF EXISTS' : ''} ${quoteSQLite(t)}`,
  dropView: (_s, v, ie) => `DROP VIEW${ie !== false ? ' IF EXISTS' : ''} ${quoteSQLite(v)}`,
  createViewDDL: (_s, v, sql, orReplace) => `CREATE${orReplace !== false ? ' OR REPLACE' : ''} VIEW ${quoteSQLite(v)} AS\n${sql}`,
  createTableDDL: (p) => {
    const q = quoteSQLite;
    const lines: string[] = [];
    for (const col of p.columns) {
      let def = `  ${q(col.name)} ${col.type || 'TEXT'}`;
      if (!col.nullable) def += ' NOT NULL';
      if (col.defaultValue) def += ` DEFAULT ${quoteDefaultValue(col.defaultValue)}`;
      lines.push(def);
    }
    if (p.primaryKey) lines.push(`  PRIMARY KEY (${q(p.primaryKey)})`);
    return `CREATE TABLE ${q(p.table)} (\n${lines.join(',\n')}\n);`;
  },
  createDatabase: () => { throw new Error('SQLite does not support CREATE DATABASE'); },
  processColumns: () => [],
  databaseColumns: () => [],
  userColumns: () => [],
  roleColumns: () => [],
  privilegeColumns: () => [],
  tablespaceColumns: () => [],
  dropDatabase: () => { throw new Error('Not supported on SQLite'); },
  createSchema: () => { throw new Error('Not supported on SQLite'); },
  dropSchema: () => { throw new Error('Not supported on SQLite'); },
  createUser: () => { throw new Error('Not supported on SQLite'); },
  dropUser: () => { throw new Error('Not supported on SQLite'); },
  supportsIfExists: () => true,
};

// ─── Oracle ────────────────────────────────────────────────────────────────

const oracleDialect: DbDialect = {
  quoteIdent: (n) => quoteOracleFn(n),
  quoteLiteral,
  qualifiedTable: (s, t) => `${quoteOracleFn(s)}.${quoteOracleFn(t)}`,
  isNumericType: (ct) => numericRe.test(ct || ''),
  formatValue: (v, ct) => {
    if (v === '' || v == null) return 'NULL';
    if (numericRe.test(ct || '')) return v;
    return quoteLiteral(v);
  },
  selectQuery: makeSelectQuery(quoteOracleFn),
  countQuery: makeCountQuery(quoteOracleFn),
  deleteQuery: (s, t, w) => `DELETE FROM ${quoteOracleFn(s)}.${quoteOracleFn(t)} WHERE ${w}`,
  insertQuery: (s, t, cols, vals, ct) => {
    const qCols = cols.map(c => quoteOracleFn(c)).join(', ');
    const qVals = cols.map(c => vals[c] === '' || vals[c] == null ? 'NULL' : (numericRe.test(ct?.[c] || '') ? String(vals[c]) : quoteLiteral(String(vals[c])))).join(', ');
    return `INSERT INTO ${quoteOracleFn(s)}.${quoteOracleFn(t)} (${qCols}) VALUES (${qVals})`;
  },
  updateQuery: (s, t, sets, where, ct) => {
    const setParts = Object.entries(sets).map(([k, v]) => {
      if (v === '' || v == null) return `${quoteOracleFn(k)} = NULL`;
      return `${quoteOracleFn(k)} = ${numericRe.test(ct?.[k] || '') ? v : quoteLiteral(String(v))}`;
    }).join(', ');
    const whereParts = Object.entries(where).map(([k, v]) => {
      return `${quoteOracleFn(k)} = ${numericRe.test(ct?.[k] || '') ? v : quoteLiteral(String(v))}`;
    }).join(' AND ');
    return `UPDATE ${quoteOracleFn(s)}.${quoteOracleFn(t)} SET ${setParts} WHERE ${whereParts}`;
  },
  dropTable: (s, t, _ie) => `DROP TABLE ${quoteOracleFn(s)}.${quoteOracleFn(t)}`,
  dropView: (s, v, _ie) => `DROP VIEW ${quoteOracleFn(s)}.${quoteOracleFn(v)}`,
  createViewDDL: (s, v, sql, orReplace) => `CREATE${orReplace !== false ? ' OR REPLACE' : ''} VIEW ${quoteOracleFn(s)}.${quoteOracleFn(v)} AS\n${sql}`,
  createTableDDL: (p) => {
    const q = quoteOracleFn;
    const lines: string[] = [];
    for (const col of p.columns) {
      let def = `  ${q(col.name)} ${formatColumnType(col, false)}`;
      if (col.defaultValue) def += ` DEFAULT ${quoteDefaultValue(col.defaultValue)}`;
      if (!col.nullable) def += ' NOT NULL';
      lines.push(def);
    }
    if (p.primaryKey) lines.push(`  PRIMARY KEY (${q(p.primaryKey)})`);
    let ddl = `CREATE TABLE ${q(p.schema)}.${q(p.table)} (${lines.join(', ')})`;
    // Oracle uses COMMENT ON statements (semicolons as separators for multi-statement backend parsing)
    const extras: string[] = [];
    if (p.comment) extras.push(`COMMENT ON TABLE ${q(p.schema)}.${q(p.table)} IS '${p.comment.replace(/'/g, "''")}'`);
    for (const col of p.columns) {
      if (col.comment) extras.push(`COMMENT ON COLUMN ${q(p.schema)}.${q(p.table)}.${q(col.name)} IS '${col.comment.replace(/'/g, "''")}'`);
    }
    if (extras.length) ddl += ';\n' + extras.join(';\n') + ';';
    return ddl;
  },
  createDatabase: () => { throw new Error('Oracle uses CREATE USER for schemas'); },
  processColumns: () => [
    { dataIndex: 'pid', key: 'pid', title: 'SID', width: 70 }, { dataIndex: 'serial', key: 'serial', title: 'Serial#', width: 70 },
    { dataIndex: 'username', key: 'username', title: '用户', width: 100 }, { dataIndex: 'osuser', key: 'osuser', title: 'OS用户', width: 100 },
    { dataIndex: 'status', key: 'status', title: '状态', width: 80 }, { dataIndex: 'machine', key: 'machine', title: '机器', width: 120 },
    { dataIndex: 'program', key: 'program', title: '程序', width: 120 }, { dataIndex: 'sql_id', key: 'sql_id', title: 'SQL ID', width: 100 },
    { dataIndex: 'seconds', key: 'seconds', title: '运行(s)', width: 80 },
  ],
  databaseColumns: () => [
    { dataIndex: 'name', key: 'name', title: '用户名(Schema)', width: 180 }, { dataIndex: 'charset', key: 'charset', title: '字符集', width: 120 },
    { dataIndex: 'size_mb', key: 'size_mb', title: '大小(MB)', width: 100 }, { dataIndex: 'tables', key: 'tables', title: '表数量', width: 80 },
  ],
  userColumns: () => [
    { dataIndex: 'username', key: 'username', title: '用户名', width: 150 }, { dataIndex: 'account_status', key: 'account_status', title: '状态', width: 110 },
    { dataIndex: 'default_tablespace', key: 'default_tablespace', title: '默认表空间', width: 120 },
    { dataIndex: 'created', key: 'created', title: '创建时间', width: 160 },
  ],
  roleColumns: () => [
    { dataIndex: 'name', key: 'name', title: '角色名', width: 180 }, { dataIndex: 'members', key: 'members', title: '成员', width: 300 },
  ],
  privilegeColumns: () => [
    { dataIndex: 'database', key: 'database', title: 'Owner', width: 120 }, { dataIndex: 'object_type', key: 'object_type', title: '类型', width: 80 },
    { dataIndex: 'object_name', key: 'object_name', title: '对象', width: 150 }, { dataIndex: 'privileges', key: 'privileges', title: '权限', width: 250 },
  ],
  tablespaceColumns: () => [
    { dataIndex: 'name', key: 'name', title: '名称', width: 200 }, { dataIndex: 'size_mb', key: 'size_mb', title: '大小(MB)', width: 120 },
    { dataIndex: 'used_mb', key: 'used_mb', title: '已用(MB)', width: 120 }, { dataIndex: 'free_mb', key: 'free_mb', title: '空闲(MB)', width: 120 },
    { dataIndex: 'usage_pct', key: 'usage_pct', title: '使用率', width: 100 }, { dataIndex: 'max_size_mb', key: 'max_size_mb', title: '最大(MB)', width: 120 },
  ],
  dropDatabase: () => { throw new Error('Oracle uses DROP USER for schemas'); },
  createSchema: () => { throw new Error('Oracle uses CREATE USER for schemas'); },
  dropSchema: () => { throw new Error('Oracle uses DROP USER for schemas'); },
  createUser: (n, pwd) => `CREATE USER ${n.toUpperCase()} IDENTIFIED BY "${pwd || n}"`,
  dropUser: (n) => `DROP USER ${quoteOracleFn(n)} CASCADE`,
  supportsIfExists: () => false,
};

// ─── SQL Server ────────────────────────────────────────────────────────────

const mssqlDialect: DbDialect = {
  quoteIdent: (n) => quoteMssql(n),
  quoteLiteral,
  qualifiedTable: (s, t) => `${quoteMssql(s)}.${quoteMssql(t)}`,
  isNumericType: (ct) => numericRe.test(ct || ''),
  formatValue: (v, ct) => {
    if (v === '' || v == null) return 'NULL';
    if (numericRe.test(ct || '')) return v;
    // NVARCHAR/NCHAR/NTEXT need N prefix for Unicode
    if (ct && /^n(varchar|char|text)/i.test(ct)) return "N'" + v.replace(/'/g, "''") + "'";
    return quoteLiteral(v);
  },
  selectQuery: makeSelectQuery(quoteMssql),
  countQuery: makeCountQuery(quoteMssql),
  deleteQuery: (s, t, w) => `DELETE FROM ${quoteMssql(s)}.${quoteMssql(t)} WHERE ${w}`,
  insertQuery: (s, t, cols, vals, ct) => {
    const qCols = cols.map(c => quoteMssql(c)).join(', ');
    const qVals = cols.map(c => vals[c] === '' || vals[c] == null ? 'NULL' : (numericRe.test(ct?.[c] || '') ? String(vals[c]) : mssqlDialect.formatValue(String(vals[c]), ct?.[c]))).join(', ');
    return `INSERT INTO ${quoteMssql(s)}.${quoteMssql(t)} (${qCols}) VALUES (${qVals})`;
  },
  updateQuery: (s, t, sets, where, ct) => {
    const setParts = Object.entries(sets).map(([k, v]) => {
      if (v === '' || v == null) return `${quoteMssql(k)} = NULL`;
      return `${quoteMssql(k)} = ${mssqlDialect.formatValue(String(v), ct?.[k])}`;
    }).join(', ');
    const whereParts = Object.entries(where).map(([k, v]) => {
      return `${quoteMssql(k)} = ${mssqlDialect.formatValue(String(v), ct?.[k])}`;
    }).join(' AND ');
    return `UPDATE ${quoteMssql(s)}.${quoteMssql(t)} SET ${setParts} WHERE ${whereParts}`;
  },
  dropTable: (s, t, _ie) => `DROP TABLE IF EXISTS ${quoteMssql(s)}.${quoteMssql(t)}`,
  dropView: (s, v, _ie) => `DROP VIEW IF EXISTS ${quoteMssql(s)}.${quoteMssql(v)}`,
  createViewDDL: (s, v, sql, orReplace) => {
    const qv = `${quoteMssql(s)}.${quoteMssql(v)}`;
    const verb = orReplace !== false ? 'CREATE OR ALTER VIEW' : 'CREATE VIEW';
    return `${verb} ${qv} AS\n${sql}`;
  },
  createTableDDL: (p) => {
    const q = quoteMssql;
    const lines: string[] = [];
    for (const col of p.columns) {
      let def = `  ${q(col.name)} ${formatColumnType(col, true)}`;
      if (!col.nullable) def += ' NOT NULL';
      if (col.defaultValue) def += ` DEFAULT ${quoteDefaultValue(col.defaultValue)}`;
      lines.push(def);
    }
    if (p.primaryKey) lines.push(`  PRIMARY KEY (${q(p.primaryKey)})`);
    let ddl = `CREATE TABLE ${q(p.schema)}.${q(p.table)} (\n${lines.join(',\n')}\n);`;
    // SQL Server uses extended properties for comments
    const cleanTable = p.table.replace(/'/g, "''");
    const xtra: string[] = [];
    if (p.comment) {
      xtra.push(
        `EXEC('IF EXISTS (SELECT 1 FROM sys.extended_properties WHERE major_id = OBJECT_ID(''${q(p.schema)}.${q(p.table)}'') AND minor_id = 0 AND name = ''MS_Description'') ` +
        `EXEC sys.sp_dropextendedproperty @name=N''MS_Description'', @level0type=N''SCHEMA'', @level0name=N''${p.schema}'', @level1type=N''TABLE'', @level1name=N''${cleanTable}''; ` +
        `EXEC sys.sp_addextendedproperty @name=N''MS_Description'', @value=N''${p.comment.replace(/'/g, "''")}'', @level0type=N''SCHEMA'', @level0name=N''${p.schema}'', @level1type=N''TABLE'', @level1name=N''${cleanTable}'';')`
      );
    }
    for (const col of p.columns) {
      if (col.name && col.comment) {
        const cc = col.name.replace(/'/g, "''");
        xtra.push(
          `EXEC('IF EXISTS (SELECT 1 FROM sys.extended_properties WHERE major_id = OBJECT_ID(''${q(p.schema)}.${q(p.table)}'') AND minor_id = COLUMNPROPERTY(OBJECT_ID(''${q(p.schema)}.${q(p.table)}''), ''${cc}'', ''ColumnId'') AND name = ''MS_Description'') ` +
          `EXEC sys.sp_dropextendedproperty @name=N''MS_Description'', @level0type=N''SCHEMA'', @level0name=N''${p.schema}'', @level1type=N''TABLE'', @level1name=N''${cleanTable}'', @level2type=N''COLUMN'', @level2name=N''${cc}''; ` +
          `EXEC sys.sp_addextendedproperty @name=N''MS_Description'', @value=N''${col.comment.replace(/'/g, "''")}'', @level0type=N''SCHEMA'', @level0name=N''${p.schema}'', @level1type=N''TABLE'', @level1name=N''${cleanTable}'', @level2type=N''COLUMN'', @level2name=N''${cc}'';')`
        );
      }
    }
    if (xtra.length) ddl += '\n' + xtra.join('\n');
    return ddl;
  },
  createDatabase: (n) => `CREATE DATABASE ${quoteMssql(n)}`,
  processColumns: () => [
    { dataIndex: 'pid', key: 'pid', title: 'Session', width: 70 }, { dataIndex: 'username', key: 'username', title: '用户', width: 120 },
    { dataIndex: 'status', key: 'status', title: '状态', width: 80 }, { dataIndex: 'host_name', key: 'host_name', title: '主机', width: 120 },
    { dataIndex: 'program_name', key: 'program_name', title: '程序', width: 120 }, { dataIndex: 'seconds', key: 'seconds', title: '运行(s)', width: 80 },
    { dataIndex: 'query', key: 'query', title: 'SQL', width: 300 },
  ],
  databaseColumns: () => [
    { dataIndex: 'name', key: 'name', title: '数据库名称', width: 180 }, { dataIndex: 'charset', key: 'charset', title: '排序规则', width: 150 },
    { dataIndex: 'size_mb', key: 'size_mb', title: '大小(MB)', width: 100 }, { dataIndex: 'tables', key: 'tables', title: '表数量', width: 80 },
  ],
  userColumns: () => [
    { dataIndex: 'username', key: 'username', title: '用户名', width: 150 }, { dataIndex: 'type_desc', key: 'type_desc', title: '类型', width: 120 },
    { dataIndex: 'account_status', key: 'account_status', title: '状态', width: 80 }, { dataIndex: 'default_schema_name', key: 'default_schema_name', title: '默认架构', width: 120 },
    { dataIndex: 'is_superuser', key: 'is_superuser', title: '管理员', width: 70 },
  ],
  roleColumns: () => [
    { dataIndex: 'name', key: 'name', title: '角色名', width: 180 }, { dataIndex: 'members', key: 'members', title: '成员', width: 300 },
  ],
  privilegeColumns: () => [
    { dataIndex: 'database', key: 'database', title: '数据库', width: 120 }, { dataIndex: 'object_type', key: 'object_type', title: '类型', width: 80 },
    { dataIndex: 'object_name', key: 'object_name', title: '对象', width: 150 }, { dataIndex: 'privileges', key: 'privileges', title: '权限', width: 250 },
  ],
  tablespaceColumns: () => [],
  dropDatabase: (n) => `DROP DATABASE IF EXISTS ${quoteMssql(n)}`,
  createSchema: (s) => `CREATE SCHEMA ${quoteMssql(s)}`,
  dropSchema: (s) => `DROP SCHEMA IF EXISTS ${quoteMssql(s)}`,
  createUser: () => { throw new Error('Not supported on SQL Server'); },
  dropUser: () => { throw new Error('Not supported on SQL Server'); },
  supportsIfExists: () => true,
};

// ─── Factory ───────────────────────────────────────────────────────────────

const dialects: Record<string, DbDialect> = {
  mysql: mysqlDialect, mariadb: mysqlDialect, oceanbase: mysqlDialect,
  postgres: pgDialect, postgresql: pgDialect,
  oracle: oracleDialect,
  sqlserver: mssqlDialect,
  sqlite: sqliteDialect,
};

/** Get the DbDialect for a given database type string. */
export function getDialect(dbType: string): DbDialect {
  return dialects[dbType] || mysqlDialect;
}

// ─── SQL Classification ─────────────────────────────────────────────────────

export type SQLCategory = 'data' | 'meta' | 'other';

const kwRe = /^\s*(?:\/\*[\s\S]*?\*\/\s*)*([a-zA-Z_]\w*)(?:\s|$)/;
const withDmlRe = /^with\s[\s\S]*?\b(insert|update|delete|merge)\b/i;

const metaKeywordsByDialect: Record<string, string[]> = {
  mysql:      ['show', 'describe', 'desc', 'explain'],
  mariadb:    ['show', 'describe', 'desc', 'explain'],
  oceanbase:  ['show', 'describe', 'desc', 'explain'],
  postgres:   ['show', 'describe', 'desc', 'explain'],
  postgresql: ['show', 'describe', 'desc', 'explain'],
  oracle:     ['show', 'describe', 'desc', 'explain'],
  sqlserver:  ['explain'],
  sqlite:     [],
};

function extractFirstStatement(sql: string): string {
  let inSingle = false, inDouble = false;
  let inLineComment = false, inBlockComment = false;
  for (let i = 0; i < sql.length; i++) {
    const ch = sql[i], next = sql[i + 1];
    if (inLineComment) { if (ch === '\n') inLineComment = false; continue; }
    if (inBlockComment) { if (ch === '*' && next === '/') { inBlockComment = false; i++; } continue; }
    if (inSingle) { if (ch === "'" && next === "'") { i++; continue; } if (ch === "'") inSingle = false; continue; }
    if (inDouble) { if (ch === '"') inDouble = false; continue; }
    if (ch === '-' && next === '-') { inLineComment = true; i++; continue; }
    if (ch === '/' && next === '*') { inBlockComment = true; i++; continue; }
    if (ch === "'") { inSingle = true; continue; }
    if (ch === '"') { inDouble = true; continue; }
    if (ch === ';') return sql.substring(0, i);
  }
  return sql;
}

export function classifySQL(sql: string, dbType: string): SQLCategory {
  const dialect = dialectOf(dbType);
  const firstStmt = extractFirstStatement(sql).trim();
  if (!firstStmt) return 'other';

  const cleaned = firstStmt.replace(/^\s*(?:\/\*[\s\S]*?\*\/\s*)*/, '').trim();
  const lower = cleaned.toLowerCase();

  const kwMatch = lower.match(kwRe);
  if (!kwMatch) return 'other';
  const firstKw = kwMatch[1];

  if (firstKw === 'select') return 'data';

  if (firstKw === 'with') {
    return withDmlRe.test(lower) ? 'other' : 'data';
  }

  const key = dbType.toLowerCase();
  const metaKws = metaKeywordsByDialect[key] ?? metaKeywordsByDialect[dialect] ?? [];
  if (metaKws.includes(firstKw)) return 'meta';

  if (dialect === 'sqlserver' && (firstKw === 'exec' || firstKw === 'execute')) {
    if (/^(?:exec|execute)\s+sp_/i.test(cleaned)) return 'meta';
  }

  return 'other';
}
