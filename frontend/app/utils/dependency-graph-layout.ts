import dagre from '@dagrejs/dagre'
import { MarkerType, Position } from '@vue-flow/core'
import type { Edge as FlowEdge, Node as FlowNode } from '@vue-flow/core'

export interface GraphNode {
  id: string
  kind: 'service' | 'network' | 'volume'
  image?: string
  slug?: string
  status?: string
  health?: string
  external: boolean
}

export interface GraphEdge {
  source: string
  target: string
  type: 'depends_on' | 'network' | 'volume'
  label?: string
}

export interface DependencyGraph {
  nodes: GraphNode[]
  edges: GraphEdge[]
}

const NODE_WIDTH: Record<GraphNode['kind'], number> = {
  service: 220,
  network: 160,
  volume: 160,
}
const NODE_HEIGHT: Record<GraphNode['kind'], number> = {
  service: 68,
  network: 64,
  volume: 64,
}

const ROW_GAP = 60
const NODE_GAP = 30
const GROUP_GAP = 120

// layoutDependencyGraph positions services with dagre (driven by depends_on),
// then pins volumes and networks to a fixed row below the services --
// volumes left-aligned under the leftmost service, networks right-aligned
// under the rightmost one -- so the two resource kinds are always easy to
// find regardless of how the service graph itself is shaped.
export function layoutDependencyGraph(graph: DependencyGraph): { nodes: FlowNode[], edges: FlowEdge[] } {
  const serviceNodes = graph.nodes.filter(n => n.kind === 'service')
  const volumeNodes = graph.nodes.filter(n => n.kind === 'volume')
  const networkNodes = graph.nodes.filter(n => n.kind === 'network')

  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'LR', nodesep: 40, ranksep: 90 })
  g.setDefaultEdgeLabel(() => ({}))

  for (const node of serviceNodes) {
    g.setNode(node.id, { width: NODE_WIDTH.service, height: NODE_HEIGHT.service })
  }
  for (const edge of graph.edges) {
    if (edge.type === 'depends_on') {
      g.setEdge(edge.source, edge.target)
    }
  }
  dagre.layout(g)

  const positions = new Map<string, { x: number, y: number }>()
  let minX = 0
  let maxX = 0
  let maxY = 0
  let hasService = false

  for (const node of serviceNodes) {
    const pos = g.node(node.id) ?? { x: 0, y: 0 }
    const x = pos.x - NODE_WIDTH.service / 2
    const y = pos.y - NODE_HEIGHT.service / 2
    positions.set(node.id, { x, y })
    minX = hasService ? Math.min(minX, x) : x
    maxX = hasService ? Math.max(maxX, x + NODE_WIDTH.service) : x + NODE_WIDTH.service
    maxY = hasService ? Math.max(maxY, y + NODE_HEIGHT.service) : y + NODE_HEIGHT.service
    hasService = true
  }

  const belowY = maxY + ROW_GAP

  // Order each resource row by the average x of the services it connects to
  // (barycenter heuristic) instead of input order -- this keeps a resource
  // roughly underneath the services that use it, so edges run near-vertical
  // instead of criss-crossing the whole row to reach a fixed left/right slot.
  const barycenterX = (nodeId: string): number => {
    const connected = graph.edges
      .filter(e => (e.source === nodeId || e.target === nodeId))
      .map(e => positions.get(e.source === nodeId ? e.target : e.source))
      .filter((p): p is { x: number, y: number } => !!p)
    if (connected.length === 0) return minX
    return connected.reduce((sum, p) => sum + p.x, 0) / connected.length
  }

  const orderedVolumes = [...volumeNodes].sort((a, b) => barycenterX(a.id) - barycenterX(b.id))
  const orderedNetworks = [...networkNodes].sort((a, b) => barycenterX(a.id) - barycenterX(b.id))

  let volX = minX
  for (const node of orderedVolumes) {
    positions.set(node.id, { x: volX, y: belowY })
    volX += NODE_WIDTH.volume + NODE_GAP
  }
  // volX now sits one NODE_GAP past the last volume's right edge -- back
  // that off to get the group's actual right edge, then require the network
  // group to start at least GROUP_GAP past it so the two groups never touch,
  // even when a narrow service row would otherwise pull them together.
  const volEndX = orderedVolumes.length > 0 ? volX - NODE_GAP : minX

  const netTotalWidth = orderedNetworks.length * NODE_WIDTH.network + Math.max(0, orderedNetworks.length - 1) * NODE_GAP
  let netX = Math.max(maxX - netTotalWidth, volEndX + GROUP_GAP)
  for (const node of orderedNetworks) {
    positions.set(node.id, { x: netX, y: belowY })
    netX += NODE_WIDTH.network + NODE_GAP
  }

  const nodes: FlowNode[] = graph.nodes.map(node => {
    const isService = node.kind === 'service'
    return {
      id: node.id,
      type: node.kind,
      position: positions.get(node.id) ?? { x: 0, y: 0 },
      data: node,
      sourcePosition: isService ? Position.Right : Position.Bottom,
      targetPosition: isService ? Position.Left : Position.Top,
    }
  })

  // network and volume edges get distinct colors matching their node kind
  // (info blue / violet) so the line itself -- not just the icon at its end
  // -- tells you which kind of resource it's tracing back to.
  const edgeColor: Record<GraphEdge['type'], string> = {
    depends_on: 'var(--ui-color-amber-500, #f59e0b)',
    network: 'var(--ui-color-info-400)',
    volume: 'var(--ui-color-violet-400, #a78bfa)',
  }

  const edges: FlowEdge[] = graph.edges.map((edge, idx) => ({
    id: `e${idx}-${edge.source}-${edge.target}-${edge.type}`,
    source: edge.source,
    target: edge.target,
    // depends_on edges are usually one-to-one between two services, so a
    // right-angle smoothstep reads cleanly. network/volume edges routinely
    // fan out from one service to several resources (or converge from
    // several services to one) -- smoothstep forces them to travel straight
    // out of the shared anchor point before turning, so they visually
    // overlap for that whole straight run. A bezier curve bends toward its
    // own target immediately, so parallel edges separate right away instead.
    type: edge.type === 'depends_on' ? 'smoothstep' : 'default',
    animated: false,
    label: edge.type === 'depends_on' ? 'Depends On' : edge.label,
    data: edge,
    class: `graph-edge graph-edge--${edge.type}`,
    style: edge.type === 'depends_on'
      ? { stroke: edgeColor.depends_on, strokeWidth: 2 }
      : { stroke: edgeColor[edge.type], strokeWidth: 1.5, strokeDasharray: edge.type === 'volume' ? '2 4' : '6 3' },
    markerEnd: edge.type === 'depends_on'
      ? { type: MarkerType.ArrowClosed, color: edgeColor.depends_on, width: 18, height: 18 }
      : undefined,
    labelBgPadding: [8, 4],
    labelBgBorderRadius: 4,
    labelStyle: { fill: 'var(--ui-text-highlighted)', fontWeight: 500 },
    labelBgStyle: { fill: 'var(--ui-bg-elevated)', stroke: 'var(--ui-border)', strokeWidth: 1 },
  }))

  return { nodes, edges }
}
