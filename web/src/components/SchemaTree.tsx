import React, { useEffect, useState, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Tree, Spin, Empty, Button, Input, Dropdown } from 'antd';
import {
  CloudServerOutlined,
  DatabaseOutlined,
  UserOutlined,
  ClusterOutlined,
  TableOutlined,
  EyeOutlined,
  PlusOutlined,
  ReloadOutlined,
  SearchOutlined,
  MoreOutlined,
  CopyOutlined,
  ExportOutlined,
  DeleteOutlined,
  EditOutlined,
  CodeOutlined,
} from '@ant-design/icons';
import type { DataNode } from 'antd/es/tree';
import { dsAPI } from '../api';

// ─── Types ──────────────────────────────────────────────────────────────────

export interface TreeLevelInfo {
  key: string;
  label: string;
  label_key: string;
  placeholder_key?: string;
  icon?: string;
}

export interface SystemFilter {
  exclude_names: string[];
  exclude_prefixes: string[];
  exclude_patterns: string[];
}

export interface TreeMetadata {
  db_type: string;
  levels: TreeLevelInfo[];
  allow_create: Record<string, boolean>;
  system_filter?: SystemFilter;
}

// ─── Icon resolver ──────────────────────────────────────────────────────────

const ICON_MAP: Record<string, React.ReactNode> = {
  CloudServerOutlined: <CloudServerOutlined />,
  DatabaseOutlined: <DatabaseOutlined />,
  UserOutlined: <UserOutlined />,
  ClusterOutlined: <ClusterOutlined />,
  TableOutlined: <TableOutlined />,
  EyeOutlined: <EyeOutlined />,
};

function resolveIcon(iconName?: string): React.ReactNode {
  return iconName ? ICON_MAP[iconName] ?? <DatabaseOutlined /> : <DatabaseOutlined />;
}

// Ref to access onSchemaAction from the static renderSchemaTitle helper
const schemaActionRef: React.MutableRefObject<((action: string, schema: string, database?: string) => void) | null> = { current: null };
const trRef: React.MutableRefObject<((key: string) => string) | null> = { current: null };
const dbTypeRef: React.MutableRefObject<string> = { current: 'mysql' };

// Render a schema/database/user node title with three-dot dropdown.
// nodeType: 'database', 'schema', or 'user' — determines which menu items appear.
function renderSchemaTitle(name: string, nodeType: 'database' | 'schema' | 'user', database?: string): React.ReactNode {
  const tr = trRef.current || ((k: string) => k);
  const dt = dbTypeRef.current;
  const _isOracle = dt === 'oracle';
  void _isOracle;
  const isTwoLevel = dt === 'postgres' || dt === 'sqlserver';

  // Build menu items based on node type
  const menuItems: any[] = [];
  if (nodeType === 'database') {
    if (isTwoLevel) {
      // PG/SQL Server: database node → Create Schema, Edit, Delete
      menuItems.push({ key: 'create-schema', label: tr('query.createSchema'), icon: <PlusOutlined /> });
      menuItems.push({ type: 'divider' as const });
      menuItems.push({ key: 'edit-schema', label: tr('common.edit'), icon: <EditOutlined /> });
      menuItems.push({ key: 'delete-schema', label: tr('common.delete'), danger: true, icon: <DeleteOutlined /> });
    } else if (dt === 'mysql') {
      // MySQL: database = schema → Create Table, Create View, Edit, Delete
      menuItems.push({ key: 'create-table', label: tr('query.createTable'), icon: <TableOutlined /> });
      menuItems.push({ key: 'create-view', label: tr('query.createView'), icon: <EyeOutlined /> });
      menuItems.push({ type: 'divider' as const });
      menuItems.push({ key: 'edit-schema', label: tr('common.edit'), icon: <EditOutlined /> });
      menuItems.push({ key: 'delete-schema', label: tr('common.delete'), danger: true, icon: <DeleteOutlined /> });
    }
  } else if (nodeType === 'schema') {
    // PG/SQL Server: schema node → Create Table, Create View, (PG: Edit Schema), Delete Schema
    menuItems.push({ key: 'create-table', label: tr('query.createTable'), icon: <TableOutlined /> });
    menuItems.push({ key: 'create-view', label: tr('query.createView'), icon: <EyeOutlined /> });
    menuItems.push({ type: 'divider' as const });
    if (dt === 'postgres' || dt === 'sqlserver') {
      menuItems.push({ key: 'edit-schema', label: tr('common.edit'), icon: <EditOutlined /> });
    }
    menuItems.push({ key: 'delete-schema', label: tr('common.delete'), danger: true, icon: <DeleteOutlined /> });
  } else if (nodeType === 'user') {
    // Oracle: user node → Create Table, Create View, Edit, Delete
    menuItems.push({ key: 'create-table', label: tr('query.createTable'), icon: <TableOutlined /> });
    menuItems.push({ key: 'create-view', label: tr('query.createView'), icon: <EyeOutlined /> });
    menuItems.push({ type: 'divider' as const });
    menuItems.push({ key: 'edit-schema', label: tr('common.edit'), icon: <EditOutlined /> });
    menuItems.push({ key: 'delete-schema', label: tr('common.delete'), danger: true, icon: <DeleteOutlined /> });
  }

  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
      <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{name}</span>
      {menuItems.length > 0 && (
        <Dropdown
          menu={{
            items: menuItems,
            onClick: ({ key: actionKey }: { key: string }) => {
              schemaActionRef.current?.(actionKey, name, database);
            },
          }}
          trigger={['click']}
          placement="bottomRight"
        >
          <Button
            type="text"
            size="small"
            icon={<MoreOutlined style={{ fontSize: 14 }} />}
            onClick={(e) => e.stopPropagation()}
            style={{ flexShrink: 0, opacity: 0.5 }}
          />
        </Dropdown>
      )}
    </div>
  );
}

// ─── Props ──────────────────────────────────────────────────────────────────

interface SchemaTreeProps {
  dataSourceId: string;
  selectedKey?: string;
  onSelect?: (key: string, context: { database?: string; schema?: string; user?: string; table?: string; isView?: boolean }) => void;
  onCreate?: (levelKey: string, parentName?: string) => void;
  refreshTrigger?: number;
  /** Show tables/views under schema nodes. When false, tree stops at schema level. Default: true */
  showTables?: boolean;
  /** Callback when a table action is triggered from the three-dot menu */
  onTableAction?: (action: string, schema: string, table: string, isView: boolean, database?: string) => void;
  /** Callback when a schema/database/user action is triggered from the three-dot menu */
  onSchemaAction?: (action: string, schema: string, database?: string) => void;
}

// ─── Component ──────────────────────────────────────────────────────────────

const SchemaTree: React.FC<SchemaTreeProps> = ({
  dataSourceId,
  selectedKey,
  onSelect,
  onCreate,
  refreshTrigger = 0,
  showTables = true,
  onTableAction,
  onSchemaAction,
}) => {
  const { t: tr } = useTranslation();
  const [meta, setMeta] = useState<TreeMetadata | null>(null);
  const [loading, setLoading] = useState(true);
  const [treeDataRaw, setTreeDataRaw] = useState<DataNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [searchText, setSearchText] = useState('');
  const loadedRef = React.useRef<Set<string>>(new Set());

  // Sync the schemaActionRef so renderSchemaTitle can access onSchemaAction
  schemaActionRef.current = onSchemaAction || null;
  trRef.current = tr;
  dbTypeRef.current = meta?.db_type || 'mysql';

  // Get the first non-server, non-table/views folder level (for fallback labels)
  const secondLevel = useMemo(() => {
    if (!meta?.levels) return null;
    for (const l of meta.levels) {
      if (l.key !== 'server' && l.key !== 'tables_folder' && l.key !== 'views_folder' && l.key !== 'table' && l.key !== 'view') {
        return l;
      }
    }
    return null;
  }, [meta]);

  // Get the first level that allows creation (for the "+" button)
  const creatableLevel = useMemo(() => {
    if (!meta?.levels || !meta?.allow_create) return null;
    for (const l of meta.levels) {
      if (meta.allow_create[l.key]) return l;
    }
    return null;
  }, [meta]);

  // Load tree metadata
  const loadMeta = useCallback(async () => {
    if (!dataSourceId) return;
    setLoading(true);
    try {
      const res = await (dsAPI as any).treeMetadata(dataSourceId);
      const m: TreeMetadata = res.data?.data || res.data;
      setMeta(m);
      return m;
    } catch {
      return null;
    } finally {
      setLoading(false);
    }
  }, [dataSourceId]);

  // Load root nodes (databases/schemas/users)
  const loadRootNodes = useCallback(async (m: TreeMetadata) => {
    if (!dataSourceId || !m) return;
    try {
      let names: string[];
      let levelInfo: any;

      // PG/MSSQL: root level = databases
      if (m.db_type === 'postgres' || m.db_type === 'sqlserver') {
        const res = await (dsAPI as any).databases(dataSourceId);
        names = Array.isArray(res.data) ? res.data : res.data?.data || [];
        levelInfo = { key: 'database', label: 'Database', icon: 'DatabaseOutlined' };
      } else {
        // MySQL: root = databases, Oracle: root = users
        const res = await dsAPI.schemaNames(dataSourceId);
        names = Array.isArray(res.data)
          ? res.data
          : res.data?.data || res.data?.list || [];
        // Compute level from meta.levels directly (not from stale state)
        let lvl = { key: 'database', label: 'Database', icon: 'DatabaseOutlined' };
        if (m.levels) {
          for (const l of m.levels) {
            if (l.key !== 'server' && l.key !== 'tables_folder' && l.key !== 'views_folder' && l.key !== 'table' && l.key !== 'view') {
              lvl = { key: l.key, label: l.label || '', icon: l.icon || '' };
              break;
            }
          }
        }
        levelInfo = lvl;
      }

      const nodes: DataNode[] = names.map((name) => ({
        title: onSchemaAction ? renderSchemaTitle(name, (levelInfo.key === 'user' ? 'user' : 'database')) : name,
        key: `${levelInfo.key}-${name}`,
        icon: resolveIcon(levelInfo.icon),
        isLeaf: !showTables && (m.db_type === 'mysql' || m.db_type === 'oracle'),
      }));

      setTreeDataRaw(nodes);
    } catch {
      setTreeDataRaw([]);
    }
  }, [dataSourceId, secondLevel, showTables, onSchemaAction]);

  // Initial load
  useEffect(() => {
    if (!dataSourceId) {
      setMeta(null);
      setTreeDataRaw([]);
      return;
    }
    let cancelled = false;
    setTreeDataRaw([]); // Clear old data while loading new
    (async () => {
      const m = await loadMeta();
      if (cancelled || !m) return;
      await loadRootNodes(m);
      loadedRef.current.clear();
      setExpandedKeys([]);
    })();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dataSourceId, refreshTrigger]);

  // Load children on expand
  const onLoadData = useCallback(
    async (node: DataNode): Promise<void> => {
      const key = String(node.key);

      const parts = key.split('-');
      const nodeType = parts[0];
      const nodeName = parts.slice(1).join('-');

      // Ant Design Tree caches children automatically — always load when expand fires

      try {
        if (nodeType === 'database') {
          // MySQL: database = schema, load tables/views directly
          // PG/MSSQL: database → schema → tables/views
          if (meta?.db_type === 'postgres' || meta?.db_type === 'sqlserver') {
            // Load schemas within this database
            const res = await (dsAPI as any).databaseSchemas(dataSourceId, nodeName);
            const schemaNames: string[] = Array.isArray(res.data) ? res.data : res.data?.data || [];
            const children: DataNode[] = schemaNames.map((s: string) => ({
              title: onSchemaAction ? renderSchemaTitle(s, 'schema', nodeName) : s,
              key: `schema-${nodeName}-${s}`,
              icon: <ClusterOutlined />,
              isLeaf: !showTables,
            }));
            setTreeDataRaw((prev) =>
              updateTreeNodeChildren(prev, key, children)
            );
            loadedRef.current.add(key);
            return;
          }
          // MySQL: fall through to load tables/views
        }
        if (nodeType === 'database' || nodeType === 'schema' || nodeType === 'user') {
          // For PG/MSSQL schema nodes, extract database name from key (schema-database-schemaName)
          let dbParam: string | undefined;
          let schemaName = nodeName;
          if (nodeType === 'schema' && (meta?.db_type === 'postgres' || meta?.db_type === 'sqlserver')) {
            const keyParts = key.split('-');
            // key: schema-{database}-{schemaName...}
            if (keyParts.length >= 3) {
              dbParam = keyParts[1];
              schemaName = keyParts.slice(2).join('-');
            }
          }
          const res = await dsAPI.tableList(dataSourceId, schemaName, dbParam);
          const items: any[] = Array.isArray(res.data) ? res.data : res.data?.data || res.data?.list || [];
          const tables = items.filter((i: any) => i.type === 'table');
          const views = items.filter((i: any) => i.type === 'view');

          const children: DataNode[] = [];
          // For PG/MSSQL, include the database name in folder keys so we can
          // pass it to tableList when expanding tables_folder/views_folder
          const folderSuffix = dbParam ? `${dbParam}-${schemaName}` : schemaName;
          if (tables.length > 0) {
            children.push({
              title: `Tables (${tables.length})`,
              key: `tables_folder-${folderSuffix}`,
              icon: <TableOutlined />,
              isLeaf: false,
            });
          }
          if (views.length > 0) {
            children.push({
              title: `Views (${views.length})`,
              key: `views_folder-${folderSuffix}`,
              icon: <EyeOutlined />,
              isLeaf: false,
            });
          }
          setTreeDataRaw((prev) =>
            updateTreeNodeChildren(prev, key, children.length > 0 ? children : [])
          );
        } else if (nodeType === 'tables_folder' || nodeType === 'views_folder') {
          // Extract database and schema from folder key
          // MySQL/Oracle: tables_folder-schemaName → schemaName = parts.slice(1).join("-")
          // PG/MSSQL:   tables_folder-database-schemaName
          let folderDbParam: string | undefined;
          let folderSchemaName = nodeName;
          if ((meta?.db_type === 'postgres' || meta?.db_type === 'sqlserver')) {
            const folderParts = key.split('-');
            // key: tables_folder-database-schemaName
            if (folderParts.length >= 3) {
              folderDbParam = folderParts[1];
              folderSchemaName = folderParts.slice(2).join('-');
            }
          }
          const res = await dsAPI.tableList(dataSourceId, folderSchemaName, folderDbParam);
          const items: any[] = Array.isArray(res.data) ? res.data : res.data?.data || res.data?.list || [];
          const filtered = items.filter((i: any) =>
            nodeType === 'tables_folder' ? i.type === 'table' : i.type === 'view'
          );

          const children: DataNode[] = filtered.map((item: any) => {
            const isView = item.type === 'view';
            // For PG/MSSQL, include database in key: table-database-schemaName-tableName
            const key = folderDbParam
              ? `${item.type}-${folderDbParam}-${folderSchemaName}-${item.name}`
              : `${item.type}-${folderSchemaName}-${item.name}`;
            return {
              title: onTableAction ? (
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
                  <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{item.name}</span>
                  <Dropdown
                    menu={{
                      items: [
                        { key: 'structure', label: isView ? tr('query.viewDefinition') : tr('query.viewStructure'), icon: <CodeOutlined /> },
                        { key: 'copy-ddl', label: tr('query.copyDdl'), icon: <CopyOutlined /> },
                        { key: 'export', label: tr('query.exportTable'), icon: <ExportOutlined /> },
                        { type: 'divider' as const },
                        { key: 'delete', label: isView ? tr('query.deleteView') : tr('query.deleteTable'), danger: true, icon: <DeleteOutlined /> },
                      ],
                      onClick: ({ key: actionKey }: { key: string }) => {
                        onTableAction(actionKey, folderSchemaName, item.name, isView, folderDbParam);
                      },
                    }}
                    trigger={['click']}
                    placement="bottomRight"
                  >
                    <Button
                      type="text"
                      size="small"
                      icon={<MoreOutlined style={{ fontSize: 14 }} />}
                      onClick={(e) => e.stopPropagation()}
                      style={{ flexShrink: 0, opacity: 0.5 }}
                    />
                  </Dropdown>
                </div>
              ) : (
                item.name
              ),
              key,
              icon: isView ? <EyeOutlined /> : <TableOutlined />,
              isLeaf: true,
            };
          });

          setTreeDataRaw((prev) => updateTreeNodeChildren(prev, key, children));
        }

        loadedRef.current.add(key);
      } catch {
        // Ignore load errors
      }
    },
    [dataSourceId, onTableAction, tr, meta, showTables]
  );

  function updateTreeNodeChildren(nodes: DataNode[], targetKey: string, children: DataNode[]): DataNode[] {
    return nodes.map((node) => {
      if (node.key === targetKey) return { ...node, children: children.length > 0 ? children : undefined };
      if (node.children) return { ...node, children: updateTreeNodeChildren(node.children, targetKey, children) };
      return node;
    });
  }

  // Handle select
  const handleSelect = useCallback(
    (_selectedKeys: React.Key[], info: any) => {
      const key = String(info.node.key);
      const parts = key.split('-');
      const nodeType = parts[0];

      // For non-leaf nodes: toggle expand on click
      if (nodeType !== 'table' && nodeType !== 'view') {
        setExpandedKeys(prev =>
          prev.includes(key) ? prev.filter(k => k !== key) : [...prev, key]
        );
      }

      if (nodeType === 'table' || nodeType === 'view') {
        // Key formats:
        //   MySQL/Oracle:  table-schemaName-tableName (3 parts)
        //   PG/MSSQL:      table-database-schemaName-tableName (4+ parts)
        if (parts.length >= 4) {
          const database = parts[1];
          const schemaName = parts[2];
          const tableName = parts.slice(3).join('-');
          onSelect?.(key, { schema: schemaName, table: tableName, isView: nodeType === 'view', database });
        } else {
          const schemaName = parts[1];
          const tableName = parts.slice(2).join('-');
          onSelect?.(key, { schema: schemaName, table: tableName, isView: nodeType === 'view' });
        }
        return;
      }

      const nodeName = parts.slice(1).join('-');

      if (nodeType === 'database') {
        onSelect?.(key, { database: nodeName });
      } else if (nodeType === 'schema') {
        // PG/MSSQL: key is schema-database-schemaName (3+ parts)
        if (parts.length >= 3) {
          onSelect?.(key, { database: parts[1], schema: parts.slice(2).join('-') });
        } else {
          onSelect?.(key, { schema: nodeName });
        }
      } else if (nodeType === 'user') {
        onSelect?.(key, { user: nodeName });
      } else {
        onSelect?.(key, {});
      }
    },
    [onSelect]
  );

  // Create action
  const handleCreate = useCallback(() => {
    if (!creatableLevel || !meta) return;
    onCreate?.(creatableLevel.key);
  }, [creatableLevel, meta, onCreate]);

  // Filter by search
  const filteredTreeData = useMemo(() => {
    if (!searchText) return treeDataRaw;
    const lower = searchText.toLowerCase();
    return filterTreeNodes(treeDataRaw, lower);
  }, [treeDataRaw, searchText]);

  // Get the textual label of a node for filtering.
  // title may be a raw string or a JSX element (when onSchemaAction/onTableAction is provided).
  function getNodeLabel(node: DataNode): string {
    if (typeof node.title === 'string') return node.title;
    // For JSX titles, fall back to extracting the name from the key.
    // Node key format: {type}-{name1}-{name2}-... (e.g. "schema-mydb-dbo", "database-mydb")
    const key = String(node.key);
    const dashIdx = key.indexOf('-');
    if (dashIdx < 0) return key;
    return key.slice(dashIdx + 1);
  }

  function filterTreeNodes(nodes: DataNode[], search: string): DataNode[] {
    return nodes
      .map((node) => {
        const label = getNodeLabel(node);
        const match = label.toLowerCase().includes(search);
        const childMatch = node.children ? filterTreeNodes(node.children, search) : [];
        if (match) return node;
        if (childMatch.length > 0) return { ...node, children: childMatch };
        return null;
      })
      .filter(Boolean) as DataNode[];
  }

  if (!dataSourceId) {
    return <Empty description="Select a data source" image={Empty.PRESENTED_IMAGE_SIMPLE} />;
  }

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <style>{`
        .schema-tree .ant-tree-treenode { width: 100%; }
        .schema-tree .ant-tree-node-content-wrapper { flex: 1 !important; }
        .schema-tree .ant-tree-title { flex: 1; display: block !important; }
      `}</style>
      {/* Header */}
      <div style={{ padding: '4px 8px', borderBottom: '1px solid #f0f0f0', display: 'flex', alignItems: 'center', gap: 4 }}>
        <Input
          size="small"
          placeholder="Search..."
          prefix={<SearchOutlined />}
          value={searchText}
          onChange={(e) => setSearchText(e.target.value)}
          allowClear
          style={{ flex: 1 }}
        />
        {onCreate && creatableLevel && (
          <Button
            type="text"
            size="small"
            icon={<PlusOutlined />}
            onClick={handleCreate}
            title={`Create ${creatableLevel.label}`}
          />
        )}
        <Button
          type="text"
          size="small"
          icon={<ReloadOutlined />}
          onClick={() => {
            loadedRef.current.clear();
            loadMeta().then((m) => m && loadRootNodes(m));
          }}
        />
      </div>

      {/* Tree */}
      <div style={{ flex: 1, overflow: 'auto', padding: 4 }}>
        {loading ? (
          <Spin style={{ display: 'block', margin: '20px auto' }} />
        ) : filteredTreeData.length === 0 ? (
          <Empty description="No items" image={Empty.PRESENTED_IMAGE_SIMPLE} style={{ marginTop: 20 }} />
        ) : (
          <Tree
            className="schema-tree"
            showIcon
            loadData={onLoadData}
            treeData={filteredTreeData}
            expandedKeys={expandedKeys}
            onExpand={(keys) => setExpandedKeys(keys)}
            selectedKeys={selectedKey ? [selectedKey] : []}
            onSelect={handleSelect}
            blockNode
          />
        )}
      </div>
    </div>
  );
};

export default SchemaTree;
