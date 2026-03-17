import { useEffect, useRef } from "react";
import * as d3 from "d3";
import type { NetworkNode, NetworkEdge, Particle } from "../types";

interface NetworkPageProps {
  nodes: NetworkNode[];
  edges: NetworkEdge[];
  particles: Particle[];
  lightMode: boolean;
}

export function NetworkPage({ nodes, edges, particles, lightMode }: NetworkPageProps) {
  const svgRef = useRef<SVGSVGElement>(null);
  const simRef = useRef<d3.Simulation<NetworkNode, NetworkEdge> | null>(null);
  const nodesRef = useRef<NetworkNode[]>([]);
  const particlesRef = useRef<Particle[]>([]);
  const animRef = useRef<number>(0);
  // Track what the simulation was built for, to avoid teardown on edge-only changes
  const nodeKeysRef = useRef("");
  const lightModeRef = useRef(lightMode);

  particlesRef.current = particles;

  // Rebuild simulation only when node list or theme changes
  const nodeKeys = nodes.map((n) => n.id).sort().join(",");
  const needsRebuild = nodeKeys !== nodeKeysRef.current || lightMode !== lightModeRef.current;

  useEffect(() => {
    if (!needsRebuild && simRef.current) return;

    nodeKeysRef.current = nodeKeys;
    lightModeRef.current = lightMode;

    const svg = d3.select(svgRef.current);
    const width = svgRef.current?.clientWidth || 800;
    const height = svgRef.current?.clientHeight || 600;

    // Preserve positions from previous simulation
    const nodeMap = new Map(nodesRef.current.map((n) => [n.id, n]));
    const simNodes: NetworkNode[] = nodes.map((n) => {
      const existing = nodeMap.get(n.id);
      return existing
        ? {
            ...n,
            x: existing.x,
            y: existing.y,
            vx: 0,
            vy: 0,
            fx: existing.fx,
            fy: existing.fy,
          }
        : { ...n };
    });
    nodesRef.current = simNodes;

    // Resolve edges
    const simEdges: NetworkEdge[] = edges.map((e) => ({
      source:
        simNodes.find(
          (n) => n.id === (typeof e.source === "string" ? e.source : e.source.id)
        ) || e.source,
      target:
        simNodes.find(
          (n) => n.id === (typeof e.target === "string" ? e.target : e.target.id)
        ) || e.target,
      count: e.count,
    }));

    // Stop previous simulation
    simRef.current?.stop();

    // Clear SVG
    svg.selectAll("*").remove();

    // Defs — glow filter
    const defs = svg.append("defs");
    const filter = defs.append("filter").attr("id", "particle-glow");
    filter.append("feGaussianBlur").attr("stdDeviation", "3").attr("result", "blur");
    const merge = filter.append("feMerge");
    merge.append("feMergeNode").attr("in", "blur");
    merge.append("feMergeNode").attr("in", "SourceGraphic");

    const edgeGroup = svg.append("g").attr("class", "edges");
    const nodeGroup = svg.append("g").attr("class", "nodes");
    svg.append("g").attr("class", "particles");

    const nodeFill = lightMode ? "#f5efe6" : "#16213e";
    const edgeStroke = lightMode ? "#d4c9b8" : "#0f3460";

    const edgeLines = edgeGroup
      .selectAll("line")
      .data(simEdges)
      .join("line")
      .attr("stroke", edgeStroke)
      .attr("stroke-width", (d: NetworkEdge) => Math.min(1 + d.count * 0.5, 4));

    const nodeGs = nodeGroup
      .selectAll("g")
      .data(simNodes)
      .join("g")
      .attr("cursor", "grab")
      .call(
        d3
          .drag<SVGGElement, NetworkNode>()
          .on("start", (event, d) => {
            if (!event.active) simRef.current?.alphaTarget(0.04).restart();
            d.fx = d.x;
            d.fy = d.y;
          })
          .on("drag", (event, d) => {
            d.fx = event.x;
            d.fy = event.y;
          })
          .on("end", (event, d) => {
            if (!event.active) simRef.current?.alphaTarget(0);
            d.fx = null;
            d.fy = null;
          }) as any
      );

    // Admin: outer ring
    nodeGs
      .filter((d: NetworkNode) => d.type === "admin")
      .append("circle")
      .attr("r", 28)
      .attr("fill", "none")
      .attr("stroke", (d: NetworkNode) => d.color)
      .attr("stroke-width", 1.5)
      .attr("stroke-dasharray", "4 2");

    nodeGs
      .append("circle")
      .attr("r", 22)
      .attr("fill", nodeFill)
      .attr("stroke", (d: NetworkNode) => d.color)
      .attr("stroke-width", 2);

    nodeGs
      .append("text")
      .text((d: NetworkNode) => d.name)
      .attr("text-anchor", "middle")
      .attr("dy", "0.35em")
      .attr("fill", (d: NetworkNode) => d.color)
      .attr("font-size", "10px")
      .attr("font-weight", "bold")
      .attr("pointer-events", "none");

    nodeGs
      .filter((d: NetworkNode) => d.type !== "user")
      .append("circle")
      .attr("cx", 16)
      .attr("cy", -16)
      .attr("r", 4)
      .attr("fill", (d: NetworkNode) => (d.status === "active" ? "#4ecdc4" : "#666"));

    const simulation = d3
      .forceSimulation<NetworkNode>(simNodes)
      .force(
        "link",
        d3.forceLink<NetworkNode, NetworkEdge>(simEdges).id((d) => d.id).distance(150)
      )
      .force("charge", d3.forceManyBody().strength(-200))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collide", d3.forceCollide(45))
      .alphaDecay(0.08)
      .velocityDecay(0.8)
      .on("tick", () => {
        edgeLines
          .attr("x1", (d: any) => d.source.x)
          .attr("y1", (d: any) => d.source.y)
          .attr("x2", (d: any) => d.target.x)
          .attr("y2", (d: any) => d.target.y);
        nodeGs.attr("transform", (d: NetworkNode) => `translate(${d.x},${d.y})`);
      });

    simRef.current = simulation;

    return () => {
      simulation.stop();
    };
  }, [needsRebuild, nodes, edges, nodeKeys, lightMode]);

  // Update edges in-place when only edge data changes (no simulation rebuild)
  useEffect(() => {
    if (!svgRef.current || !simRef.current) return;
    if (needsRebuild) return; // handled by the effect above

    const svg = d3.select(svgRef.current);
    const edgeGroup = svg.select("g.edges");
    if (edgeGroup.empty()) return;

    const edgeStroke = lightMode ? "#d4c9b8" : "#0f3460";
    const simNodes = nodesRef.current;

    // Resolve new edges to existing node refs
    const simEdges: NetworkEdge[] = edges.map((e) => ({
      source:
        simNodes.find(
          (n) => n.id === (typeof e.source === "string" ? e.source : e.source.id)
        ) || e.source,
      target:
        simNodes.find(
          (n) => n.id === (typeof e.target === "string" ? e.target : e.target.id)
        ) || e.target,
      count: e.count,
    }));

    // Update edge lines without restarting simulation
    const edgeLines = edgeGroup
      .selectAll<SVGLineElement, NetworkEdge>("line")
      .data(simEdges, (d: NetworkEdge) => {
        const sId = typeof d.source === "string" ? d.source : d.source.id;
        const tId = typeof d.target === "string" ? d.target : d.target.id;
        return [sId, tId].sort().join("-");
      });

    // Enter new edges
    edgeLines
      .enter()
      .append("line")
      .attr("stroke", edgeStroke)
      .attr("stroke-width", (d) => Math.min(1 + d.count * 0.5, 4))
      .attr("x1", (d: any) => d.source.x || 0)
      .attr("y1", (d: any) => d.source.y || 0)
      .attr("x2", (d: any) => d.target.x || 0)
      .attr("y2", (d: any) => d.target.y || 0);

    // Update existing edge widths
    edgeLines.attr("stroke-width", (d) => Math.min(1 + d.count * 0.5, 4));

    // Update simulation link force with new edges
    const linkForce = simRef.current.force("link") as d3.ForceLink<NetworkNode, NetworkEdge>;
    if (linkForce) {
      linkForce.links(simEdges);
      // Tiny nudge so new edges get positioned on next tick, but no visible movement
      simRef.current.alpha(0.01).restart();
    }
  }, [edges, needsRebuild, lightMode]);

  // Particle animation loop
  useEffect(() => {
    if (!svgRef.current) return;

    const tick = () => {
      const svg = d3.select(svgRef.current);
      const particleGroup = svg.select("g.particles");
      if (particleGroup.empty()) {
        animRef.current = requestAnimationFrame(tick);
        return;
      }

      particleGroup.selectAll("*").remove();

      const now = performance.now();
      const currentParticles = particlesRef.current;

      for (const p of currentParticles) {
        const progress = (now - p.startTime) / p.duration;
        if (progress < 0 || progress > 1) continue;

        const sourceNode = nodesRef.current.find((n) => n.id === p.source);
        const targetNode = nodesRef.current.find((n) => n.id === p.target);
        if (
          sourceNode?.x == null ||
          sourceNode?.y == null ||
          targetNode?.x == null ||
          targetNode?.y == null
        )
          continue;

        const x = sourceNode.x + (targetNode.x - sourceNode.x) * progress;
        const y = sourceNode.y + (targetNode.y - sourceNode.y) * progress;
        const opacity = progress < 0.8 ? 1 : 1 - (progress - 0.8) / 0.2;

        particleGroup
          .append("circle")
          .attr("cx", x)
          .attr("cy", y)
          .attr("r", 5)
          .attr("fill", p.color)
          .attr("opacity", opacity)
          .attr("filter", "url(#particle-glow)");
      }

      animRef.current = requestAnimationFrame(tick);
    };

    animRef.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(animRef.current);
  }, []);

  return (
    <div className="flex-1 bg-bg flex items-center justify-center overflow-hidden">
      <svg ref={svgRef} className="w-full h-full" style={{ minHeight: "100%" }} />
    </div>
  );
}
