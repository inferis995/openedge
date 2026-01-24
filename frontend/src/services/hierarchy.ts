// Hierarchical Data Service
// Design System: See tasks/design-system.md

import { getSites, getAreas, getGateways, getTags } from './api';
import type { Site, Area, GatewayWithHealth, Tag } from '@/types/api';

// Tree node types for hierarchical display
export type TreeNodeType = 'site' | 'area' | 'gateway' | 'tag';

export interface TreeNode {
  id: number;
  name: string;
  type: TreeNodeType;
  data: Site | Area | GatewayWithHealth | Tag;
  children?: TreeNode[];
  // Additional properties for UI
  isExpanded?: boolean;
  isSelected?: boolean;
  // Gateway-specific health status
  connectionStatus?: 'online' | 'offline';
  lastSeen?: number;
}

// Cache for hierarchy data to avoid excessive API calls
interface HierarchyCache {
  data: TreeNode[];
  timestamp: number;
  orgId: number | null;
}

const CACHE_TTL = 30000; // 30 seconds
let hierarchyCache: HierarchyCache | null = null;

/**
 * Fetch hierarchy for an organization and transform into tree structure
 * @param orgId - Organization ID to fetch hierarchy for
 * @param useCache - Whether to use cached data (default: true)
 * @returns Promise<TreeNode[]> - Hierarchical tree structure
 */
export async function fetchHierarchy(
  orgId: number,
  useCache: boolean = true
): Promise<TreeNode[]> {
  // Check cache first
  if (useCache && hierarchyCache && hierarchyCache.orgId === orgId) {
    const cacheAge = Date.now() - hierarchyCache.timestamp;
    if (cacheAge < CACHE_TTL) {
      return hierarchyCache.data;
    }
  }

  try {
    // Fetch all sites for the organization
    // Note: X-Organization-ID header is automatically added by axios interceptor
    const sites = await getSites(orgId);

    // Build the tree hierarchy
    const tree: TreeNode[] = [];

    for (const site of sites) {
      const siteNode: TreeNode = {
        id: site.id,
        name: site.name,
        type: 'site',
        data: site,
        children: [],
      };

      // Fetch areas for this site
      try {
        const areas = await getAreas(site.id);

        for (const area of areas) {
          const areaNode: TreeNode = {
            id: area.id,
            name: area.name,
            type: 'area',
            data: area,
            children: [],
          };

          // Fetch gateways for this area
          try {
            const gateways = await getGateways(area.id);

            for (const gateway of gateways) {
              const gatewayNode: TreeNode = {
                id: gateway.id,
                name: gateway.name,
                type: 'gateway',
                data: gateway,
                children: [],
                connectionStatus: gateway.connection_status,
                lastSeen: gateway.last_seen,
              };

              // Fetch tags for this gateway
              try {
                const tags = await getTags(gateway.id);

                for (const tag of tags) {
                  const tagNode: TreeNode = {
                    id: tag.id,
                    name: tag.alias,
                    type: 'tag',
                    data: tag,
                  };
                  gatewayNode.children!.push(tagNode);
                }
              } catch (error) {
                // Log error but continue - tags fetch failure shouldn't break entire tree
                console.error(`Failed to fetch tags for gateway ${gateway.id}:`, error);
              }

              areaNode.children!.push(gatewayNode);
            }
          } catch (error) {
            // Log error but continue - gateways fetch failure shouldn't break entire tree
            console.error(`Failed to fetch gateways for area ${area.id}:`, error);
          }

          siteNode.children!.push(areaNode);
        }
      } catch (error) {
        // Log error but continue - areas fetch failure shouldn't break entire tree
        console.error(`Failed to fetch areas for site ${site.id}:`, error);
      }

      tree.push(siteNode);
    }

    // Update cache
    hierarchyCache = {
      data: tree,
      timestamp: Date.now(),
      orgId,
    };

    return tree;
  } catch (error) {
    console.error('Failed to fetch hierarchy:', error);
    throw error;
  }
}

/**
 * Clear the hierarchy cache
 * Useful after creating/updating entities to force a refresh
 */
export function clearHierarchyCache(): void {
  hierarchyCache = null;
}

/**
 * Find a node in the tree by ID
 * @param tree - Tree structure to search
 * @param nodeId - Node ID to find
 * @returns TreeNode | null - Found node or null
 */
export function findNodeById(tree: TreeNode[], nodeId: number): TreeNode | null {
  for (const node of tree) {
    if (node.id === nodeId) {
      return node;
    }
    if (node.children && node.children.length > 0) {
      const found = findNodeById(node.children, nodeId);
      if (found) {
        return found;
      }
    }
  }
  return null;
}

/**
 * Find all nodes of a specific type
 * @param tree - Tree structure to search
 * @param type - Node type to filter by
 * @returns TreeNode[] - Array of matching nodes
 */
export function findNodesByType(tree: TreeNode[], type: TreeNodeType): TreeNode[] {
  const results: TreeNode[] = [];

  function search(nodes: TreeNode[]) {
    for (const node of nodes) {
      if (node.type === type) {
        results.push(node);
      }
      if (node.children && node.children.length > 0) {
        search(node.children);
      }
    }
  }

  search(tree);
  return results;
}

/**
 * Get the path to a node (array of parent nodes)
 * @param tree - Tree structure
 * @param nodeId - Target node ID
 * @returns TreeNode[] - Path from root to target node
 */
export function getPathToNode(tree: TreeNode[], nodeId: number): TreeNode[] {
  for (const node of tree) {
    if (node.id === nodeId) {
      return [node];
    }
    if (node.children && node.children.length > 0) {
      const path = getPathToNode(node.children, nodeId);
      if (path.length > 0) {
        return [node, ...path];
      }
    }
  }
  return [];
}

/**
 * Filter tree to show only nodes matching a search query
 * @param tree - Tree structure to filter
 * @param query - Search query string
 * @returns TreeNode[] - Filtered tree structure
 */
export function filterTree(tree: TreeNode[], query: string): TreeNode[] {
  const lowerQuery = query.toLowerCase();

  function filterNode(node: TreeNode): TreeNode | null {
    // Check if node matches query
    const matches = node.name.toLowerCase().includes(lowerQuery);

    // Recursively filter children
    const filteredChildren = node.children
      ? node.children.map(filterNode).filter((child): child is TreeNode => child !== null)
      : [];

    // Include node if it matches or has matching children
    if (matches || filteredChildren.length > 0) {
      return {
        ...node,
        children: filteredChildren,
      };
    }

    return null;
  }

  return tree
    .map(filterNode)
    .filter((node): node is TreeNode => node !== null);
}

/**
 * Count total nodes in tree (including all descendants)
 * @param tree - Tree structure
 * @returns number - Total node count
 */
export function countNodes(tree: TreeNode[]): number {
  let count = 0;

  function count(nodes: TreeNode[]) {
    for (const node of nodes) {
      count++;
      if (node.children && node.children.length > 0) {
        count(node.children);
      }
    }
  }

  count(tree);
  return count;
}

/**
 * Get statistics about the hierarchy
 * @param tree - Tree structure
 * @returns Object with counts by type
 */
export function getHierarchyStats(tree: TreeNode[]): {
  sites: number;
  areas: number;
  gateways: number;
  tags: number;
  onlineGateways: number;
  offlineGateways: number;
} {
  const stats = {
    sites: 0,
    areas: 0,
    gateways: 0,
    tags: 0,
    onlineGateways: 0,
    offlineGateways: 0,
  };

  function traverse(nodes: TreeNode[]) {
    for (const node of nodes) {
      switch (node.type) {
        case 'site':
          stats.sites++;
          break;
        case 'area':
          stats.areas++;
          break;
        case 'gateway':
          stats.gateways++;
          if (node.connectionStatus === 'online') {
            stats.onlineGateways++;
          } else if (node.connectionStatus === 'offline') {
            stats.offlineGateways++;
          }
          break;
        case 'tag':
          stats.tags++;
          break;
      }
      if (node.children && node.children.length > 0) {
        traverse(node.children);
      }
    }
  }

  traverse(tree);
  return stats;
}
