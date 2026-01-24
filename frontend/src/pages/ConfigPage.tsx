// Design System: See tasks/design-system.md
// Configuration Page with Split View (US-048)
// Layout: 30% width tree view on left, 70% detail panel on right

import { useEffect, useState } from 'react';
import { TreeView } from '@/components/TreeView';
import { DetailPanel } from '@/components/DetailPanel';
import { fetchHierarchy, clearHierarchyCache } from '@/services/hierarchy';
import type { TreeNode } from '@/services/hierarchy';
import { useToast } from '@/components/ui/use-toast';
import { Loader2, Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

export function ConfigPage() {
  const [tree, setTree] = useState<TreeNode[]>([]);
  const [selectedNodeId, setSelectedNodeId] = useState<number | null>(null);
  const [selectedNode, setSelectedNode] = useState<TreeNode | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const { toast } = useToast();

  const loadHierarchy = async () => {
    setIsLoading(true);
    try {
      // Get organization ID from localStorage
      const orgId = localStorage.getItem('ralph_org_id');
      if (!orgId) {
        toast({
          variant: 'destructive',
          title: 'No organization selected',
          description: 'Please select an organization to access the configuration.',
        });
        return;
      }

      const hierarchy = await fetchHierarchy(parseInt(orgId, 10));
      setTree(hierarchy);
    } catch (error) {
      console.error('Failed to load hierarchy:', error);
      toast({
        variant: 'destructive',
        title: 'Failed to load data',
        description: 'Could not fetch the organization hierarchy. Please try again.',
      });
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    loadHierarchy();
  }, [toast]);

  const handleNodeSelect = (node: TreeNode) => {
    setSelectedNodeId(node.id);
    setSelectedNode(node);
  };

  const handleUpdate = async () => {
    // Refresh hierarchy and reload data
    setIsRefreshing(true);
    clearHierarchyCache();
    try {
      const orgId = localStorage.getItem('ralph_org_id');
      if (orgId) {
        const hierarchy = await fetchHierarchy(parseInt(orgId, 10), false);
        setTree(hierarchy);
        // Re-select the node if it still exists
        if (selectedNodeId) {
          const findNode = (nodes: TreeNode[]): TreeNode | null => {
            for (const node of nodes) {
              if (node.id === selectedNodeId) return node;
              if (node.children) {
                const found = findNode(node.children);
                if (found) return found;
              }
            }
            return null;
          };
          const updatedNode = findNode(hierarchy);
          if (updatedNode) {
            setSelectedNode(updatedNode);
          }
        }
      }
      toast({
        title: 'Refreshed',
        description: 'Configuration data has been updated.',
      });
    } catch (error) {
      console.error('Failed to refresh hierarchy:', error);
    } finally {
      setIsRefreshing(false);
    }
  };

  const handleCreateEntity = (type: string) => {
    // TODO: Will be implemented in US-049, US-050, US-051, US-052, US-053
    toast({
      title: 'Coming Soon',
      description: `Create ${type} dialog will be implemented in an upcoming update.`,
    });
  };

  return (
    <div className="h-full flex flex-col">
      {/* Header */}
      <div className="mb-4 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Configuration</h1>
          <p className="text-sm text-gray-600 mt-1">
            {selectedNode
              ? `Selected: ${selectedNode.name} (${selectedNode.type})`
              : 'Select an item from the tree to view details'}
          </p>
        </div>
        <div className="flex gap-2">
          {/* Create buttons dropdown */}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm">
                <Plus className="h-4 w-4 mr-2" />
                Create
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => handleCreateEntity('organization')}>
                Organization
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => handleCreateEntity('site')}>
                Site
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => handleCreateEntity('area')}>
                Area
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => handleCreateEntity('gateway')}>
                Gateway
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => handleCreateEntity('tag')}>
                Tag
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button
            variant="outline"
            size="sm"
            onClick={handleUpdate}
            disabled={isRefreshing}
          >
            {isRefreshing ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Refreshing...
              </>
            ) : (
              'Refresh'
            )}
          </Button>
        </div>
      </div>

      {/* Loading State */}
      {isLoading ? (
        <div className="flex items-center justify-center flex-1">
          <div className="text-center">
            <Loader2 className="h-8 w-8 animate-spin text-violet-600 mx-auto mb-2" />
            <p className="text-gray-600">Loading configuration...</p>
          </div>
        </div>
      ) : (
        /* Split View: 30% tree, 70% detail panel */
        <div className="flex-1 flex gap-4 min-h-0">
          {/* Tree View - 30% width */}
          <div className="w-[30%] min-w-[250px] max-w-[400px] border rounded-lg bg-white shadow-sm overflow-hidden flex flex-col">
            <div className="p-3 border-b bg-gray-50">
              <h2 className="text-sm font-semibold text-gray-700">Hierarchy</h2>
            </div>
            <div className="flex-1 overflow-auto">
              <TreeView
                tree={tree}
                selectedNodeId={selectedNodeId}
                onNodeSelect={handleNodeSelect}
              />
            </div>
          </div>

          {/* Detail Panel - 70% width */}
          <div className="flex-1 min-w-0">
            <DetailPanel
              node={selectedNode}
              onUpdate={handleUpdate}
              className="h-full"
            />
          </div>
        </div>
      )}
    </div>
  );
}
