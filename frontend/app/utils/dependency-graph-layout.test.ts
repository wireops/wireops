import { describe, it, expect } from 'vitest'
import { layoutDependencyGraph } from './dependency-graph-layout'
import type { DependencyGraph } from './dependency-graph-layout'

const graph: DependencyGraph = {
  nodes: [
    { id: 'service:api', kind: 'service', image: 'api:latest', external: false },
    { id: 'service:worker', kind: 'service', image: 'worker:latest', external: false },
    { id: 'service:db', kind: 'service', image: 'postgres:16', external: false },
    { id: 'volume:data', kind: 'volume', external: false },
    { id: 'network:backend', kind: 'network', external: false },
  ],
  edges: [
    { source: 'service:api', target: 'service:db', type: 'depends_on' },
    { source: 'service:worker', target: 'service:db', type: 'depends_on' },
    { source: 'service:db', target: 'volume:data', type: 'volume' },
    { source: 'service:api', target: 'network:backend', type: 'network' },
    { source: 'service:worker', target: 'network:backend', type: 'network' },
  ],
}

describe('layoutDependencyGraph', () => {
  it('positions every node and preserves node data', () => {
    const { nodes } = layoutDependencyGraph(graph)

    expect(nodes).toHaveLength(graph.nodes.length)
    for (const node of nodes) {
      expect(Number.isFinite(node.position.x)).toBe(true)
      expect(Number.isFinite(node.position.y)).toBe(true)
    }

    const db = nodes.find(n => n.id === 'service:db')!
    expect(db.data).toEqual(graph.nodes.find(n => n.id === 'service:db'))
  })

  it('places volumes and networks on a shared row below the services', () => {
    const { nodes } = layoutDependencyGraph(graph)

    const serviceYs = nodes.filter(n => n.type === 'service').map(n => n.position.y)
    const volume = nodes.find(n => n.type === 'volume')!
    const network = nodes.find(n => n.type === 'network')!

    expect(volume.position.y).toBe(network.position.y)
    expect(volume.position.y).toBeGreaterThan(Math.max(...serviceYs))
  })

  it('orders the resource row by the barycenter of its connected services', () => {
    const branching: DependencyGraph = {
      nodes: [
        { id: 'service:left', kind: 'service', external: false },
        { id: 'service:right', kind: 'service', external: false },
        { id: 'volume:near-left', kind: 'volume', external: false },
        { id: 'volume:near-right', kind: 'volume', external: false },
      ],
      edges: [
        { source: 'service:left', target: 'service:right', type: 'depends_on' },
        { source: 'service:left', target: 'volume:near-left', type: 'volume' },
        { source: 'service:right', target: 'volume:near-right', type: 'volume' },
      ],
    }

    const { nodes } = layoutDependencyGraph(branching)
    const left = nodes.find(n => n.id === 'service:left')!
    const right = nodes.find(n => n.id === 'service:right')!
    const nearLeft = nodes.find(n => n.id === 'volume:near-left')!
    const nearRight = nodes.find(n => n.id === 'volume:near-right')!

    expect(left.position.x).toBeLessThan(right.position.x)
    // the volume barycenter-anchored to the left service sorts before the
    // one anchored to the right service in the resource row.
    expect(nearLeft.position.x).toBeLessThan(nearRight.position.x)
  })

  it('maps edge types to the expected style/marker', () => {
    const { edges } = layoutDependencyGraph(graph)

    const dependsOn = edges.find(e => e.data?.type === 'depends_on')!
    expect(dependsOn.type).toBe('smoothstep')
    expect(dependsOn.markerEnd).toBeDefined()

    const network = edges.find(e => e.data?.type === 'network')!
    expect(network.type).toBe('default')
    expect(network.markerEnd).toBeUndefined()
    expect(network.style).toMatchObject({ strokeDasharray: '6 3' })

    const volume = edges.find(e => e.data?.type === 'volume')!
    expect(volume.type).toBe('default')
    expect(volume.style).toMatchObject({ strokeDasharray: '2 4' })
  })

  it('handles a graph with no depends_on edges', () => {
    const noDeps: DependencyGraph = {
      nodes: [
        { id: 'service:solo', kind: 'service', external: false },
        { id: 'volume:v', kind: 'volume', external: false },
      ],
      edges: [
        { source: 'service:solo', target: 'volume:v', type: 'volume' },
      ],
    }

    const { nodes, edges } = layoutDependencyGraph(noDeps)

    expect(nodes).toHaveLength(2)
    expect(edges).toHaveLength(1)
    const solo = nodes.find(n => n.id === 'service:solo')!
    expect(solo.position).toEqual({ x: 0, y: 0 })
  })
})
