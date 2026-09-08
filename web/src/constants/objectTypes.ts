export const ObjectType = {
  PROCEDURE: 'procedure',
  FUNCTION: 'function',
  TRIGGER: 'trigger',
  EVENT: 'event',
  SEQUENCE: 'sequence',
  TYPE: 'type',
  SYNONYM: 'synonym',
  PACKAGE: 'package',
  MAT_VIEW: 'matview',
} as const;

export type ObjectTypeValue = (typeof ObjectType)[keyof typeof ObjectType];

export const OBJECT_TYPE_LABELS: Record<string, string> = {
  [ObjectType.PROCEDURE]: '存储过程',
  [ObjectType.FUNCTION]: '函数',
  [ObjectType.TRIGGER]: '触发器',
  [ObjectType.EVENT]: '事件',
  [ObjectType.SEQUENCE]: '序列',
  [ObjectType.TYPE]: '类型',
  [ObjectType.SYNONYM]: '同义词',
  [ObjectType.PACKAGE]: '包',
  [ObjectType.MAT_VIEW]: '物化视图',
};

export const OBJECT_TYPE_LABELS_EN: Record<string, string> = {
  [ObjectType.PROCEDURE]: 'Procedure',
  [ObjectType.FUNCTION]: 'Function',
  [ObjectType.TRIGGER]: 'Trigger',
  [ObjectType.EVENT]: 'Event',
  [ObjectType.SEQUENCE]: 'Sequence',
  [ObjectType.TYPE]: 'Type',
  [ObjectType.SYNONYM]: 'Synonym',
  [ObjectType.PACKAGE]: 'Package',
  [ObjectType.MAT_VIEW]: 'Materialized View',
};

export const OBJECT_TYPE_ICONS: Record<string, string> = {
  [ObjectType.PROCEDURE]: 'code',
  [ObjectType.FUNCTION]: 'function',
  [ObjectType.TRIGGER]: 'thunderbolt',
  [ObjectType.EVENT]: 'calendar',
  [ObjectType.SEQUENCE]: 'number',
  [ObjectType.TYPE]: 'block',
  [ObjectType.SYNONYM]: 'swap',
  [ObjectType.PACKAGE]: 'appstore',
  [ObjectType.MAT_VIEW]: 'table',
};
