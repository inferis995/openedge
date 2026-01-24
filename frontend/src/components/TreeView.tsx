// Tree View Component
// Design System: See tasks/design-system.md

import React, { useState } from 'react';
import { ChevronRight, ChevronDown, Building2, Map, Server, Cpu, Circle } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { TreeNode, TreeNodeType } from '@/services/hierarchy';

export interface TreeViewProps {
  /** Tree data structure to render */
  tree: TreeNode[];
  /** Currently selected node ID */
  selectedNodeId?: number | null;
  /** Callback when a node is clicked */
  onNodeSelect?: (node: TreeNode) => void;
  /** Optional className for styling */
  className?: string;
}

// Icon mapping for entity types
const TYPE_ICONS: Record<TreeNodeType, React.ElementType> = {
  site: Building2,
  area: Map,
  gateway: Server,
  tag: Cpu,
};

// Color classes for entity types
const TYPE_COLORS: Record<TreeNodeType, string> = {
  site: 'text-blue-500',
  area: 'text-emerald-500',
  gateway: 'text-purple-500',
  tag: 'text-orange-500',
};

interface TreeNodeComponentProps {
  node: TreeNode;
  level: number;
  isSelected: boolean;
  selectedNodeId: number | null;
  onSelect: (node: TreeNode) => void;
}

/**
 * TreeView Component
 *
 * A hierarchical tree view for navigating the organization structure.
 * Supports expand/collapse, selection, and visual indicators for different entity types.
 */
export function TreeView({ tree, selectedNodeId = null, onNodeSelect, className }: TreeViewProps) {
  if (tree.length === 0) {
    return (
      <div className={cn('flex items-center justify-center p-8 text-gray-500', className)}>
        <p className="text-sm">No data available</p>
      </div>
    );
  }

  return (
    <div className={cn('overflow-auto', className)}>
      {tree.map((node) => (
        <TreeNodeComponent
          key={`${node.type}-${node.id}`}
          node={node}
          level={0}
          isSelected={node.id === selectedNodeId}
          selectedNodeId={selectedNodeId}
          onSelect={onNodeSelect || (() => {})}
        />
      ))}
    </div>
  );
}

/**
 * Internal tree node component for recursive rendering
 */
function TreeNodeComponent({ node, level, isSelected, selectedNodeId, onSelect }: TreeNodeComponentProps) {
  const [isExpanded, setIsExpanded] = useState(node.isExpanded ?? false);
  const hasChildren = node.children && node.children.length > 0;
  const Icon = TYPE_ICONS[node.type];
  const iconColor = TYPE_COLORS[node.type];

  const handleClick = () => {
    onSelect(node);
  };

  const handleToggle = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (hasChildren) {
      setIsExpanded(!isExpanded);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      handleClick();
    } else if (e.key === 'ArrowRight' && !isExpanded && hasChildren) {
      e.preventDefault();
      setIsExpanded(true);
    } else if (e.key === 'ArrowLeft' && isExpanded) {
      e.preventDefault();
      setIsExpanded(false);
    }
  };

  return (
    <div>
      {/* Node row */}
      <div
        className={cn(
          'flex items-center gap-1.5 py-1.5 px-2 rounded-md cursor-pointer transition-colors',
          'hover:bg-gray-100 focus:outline-none focus:ring-2 focus:ring-violet-500 focus:ring-inset',
          isSelected && 'bg-violet-100 hover:bg-violet-200'
        )}
        style={{ paddingLeft: `${level * 16 + 8}px` }}
        onClick={handleClick}
        onKeyDown={handleKeyDown}
        role="treeitem"
        aria-expanded={hasChildren ? isExpanded : undefined}
        aria-selected={isSelected}
        tabIndex={0}
      >
        {/* Expand/collapse toggle */}
        <button
          className={cn(
            'flex-shrink-0 w-5 h-5 flex items-center justify-center rounded',
            'hover:bg-gray-200 transition-colors',
            !hasChildren && 'invisible'
          )}
          onClick={handleToggle}
          aria-label={isExpanded ? 'Collapse' : 'Expand'}
        >
          {hasChildren ? (
            isExpanded ? (
              <ChevronDown className="w-4 h-4 text-gray-600" />
            ) : (
              <ChevronRight className="w-4 h-4 text-gray-600" />
            )
          ) : null}
        </button>

        {/* Entity type icon */}
        <Icon className={cn('w-4 h-4 flex-shrink-0', iconColor)} />

        {/* Node name */}
        <span className="flex-1 truncate text-sm text-gray-700">{node.name}</span>

        {/* Gateway health status indicator */}
        {node.type === 'gateway' && node.connectionStatus && (
          <div className="flex-shrink-0 ml-1" title={`Status: ${node.connectionStatus}`}>
            <Circle
              className={cn(
                'w-2 h-2',
                node.connectionStatus === 'online' ? 'fill-green-500 text-green-500' : 'fill-gray-400 text-gray-400'
              )}
            />
          </div>
        )}

        {/* Child count badge (for nodes with children) */}
        {hasChildren && (
          <span className="text-xs text-gray-500 ml-1">({node.children!.length})</span>
        )}
      </div>

      {/* Children */}
      {hasChildren && isExpanded && (
        <div role="group">
          {node.children!.map((child) => (
            <TreeNodeComponent
              key={`${child.type}-${child.id}`}
              node={child}
              level={level + 1}
              isSelected={child.id === selectedNodeId}
              selectedNodeId={selectedNodeId}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export default TreeView;
