import React from 'react';
import { Tooltip } from 'antd';

export interface TabConfig {
  key: string;
  icon: React.ReactNode;
  tooltip: string;
}

interface Props {
  tabs: TabConfig[];
  activeTab: string;
  isOpen: boolean;
  onTabClick: (key: string) => void;
}

const VerticalTabBar: React.FC<Props> = ({ tabs, activeTab, isOpen, onTabClick }) => {
  return (
    <div style={{
      width: 60,
      flexShrink: 0,
      display: 'flex',
      flexDirection: 'column',
      borderRight: '1px solid #f0f0f0',
      background: '#fafafa',
      height: '100%',
    }}>
      {tabs.map(tab => {
        const active = isOpen && activeTab === tab.key;
        return (
          <Tooltip key={tab.key} title={tab.tooltip} placement="left">
            <div
              onClick={() => onTabClick(tab.key)}
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                justifyContent: 'center',
                padding: '12px 4px',
                cursor: 'pointer',
                borderLeft: active ? '3px solid #1890ff' : '3px solid transparent',
                background: active ? '#e6f7ff' : 'transparent',
                color: active ? '#1890ff' : '#666',
                transition: 'all 0.2s',
                fontSize: 20,
              }}
              onMouseEnter={e => {
                if (!active) (e.currentTarget as HTMLElement).style.background = '#f0f0f0';
              }}
              onMouseLeave={e => {
                if (!active) (e.currentTarget as HTMLElement).style.background = 'transparent';
              }}
            >
              {tab.icon}
              <span style={{ fontSize: 10, marginTop: 4, textAlign: 'center' }}>
                {tab.tooltip}
              </span>
            </div>
          </Tooltip>
        );
      })}
    </div>
  );
};

export default VerticalTabBar;
