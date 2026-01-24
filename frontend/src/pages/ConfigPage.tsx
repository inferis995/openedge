// Design System: See tasks/design-system.md
// Configuration Page with TreeView (US-046)
// Full split view will be implemented in US-048

import { useEffect, useState } from 'react';
import { TreeView } from '@/components/TreeView';
import { fetchHierarchy } from '@/services/hierarchy';
import type { TreeNode } from '@/services/hierarchy';
import { useToast } from '@/components/ui/use-toast';

export function ConfigPage() {
  const [tree, setTree] = useState<TreeNode[]>([]);
  const [selectedNodeId, setSelectedNodeId] = useState<number | null>(null);
  const [selectedNode, setSelectedNode] = useState<TreeNode | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const { toast } = useToast();

  useEffect(() => {
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

    const loadHierarchy = async () => {
      setIsLoading(true);
      try {
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

    loadHierarchy();
  }, [toast]);

  const handleNodeSelect = (node: TreeNode) => {
    setSelectedNodeId(node.id);
    setSelectedNode(node);
    console.log('Selected node:', node);
  };

  return (
    <div className="h-full">
      <div className="mb-4">
        <h1 className="text-2xl font-bold text-gray-900">Configuration</h1>
        <p className="text-sm text-gray-600 mt-1">
          {selectedNode
            ? `Selected: ${selectedNode.name} (${selectedNode.type})`
            : 'Select an item from the tree to view details'}
        </p>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center h-64">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-violet-600"></div>
        </div>
      ) : (
        <div className="border rounded-lg bg-white shadow-sm h-[calc(100vh-200px)]">
          <TreeView
            tree={tree}
            selectedNodeId={selectedNodeId}
            onNodeSelect={handleNodeSelect}
            className="h-full"
          />
        </div>
      )}
    </div>
  );
}
