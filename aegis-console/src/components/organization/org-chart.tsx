"use client";

import { useMemo, useState } from "react";
import {
  Background, Controls, Handle, MiniMap, Position, ReactFlow,
  type Edge, type Node, type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { GitBranch, Network, Users } from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/data-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { DepartmentNode } from "@/lib/api/types";
import { useDepartmentMembersQuery, useDepartmentTreeQuery } from "@/lib/org-hooks";
import { flattenDeptOptions } from "./org-shared";

/* ================================================================== */
/*  组织架构图                                                         */
/*                                                                     */
/*  两张图回答两个不同的问题：                                          */
/*    · 部门架构 —— 组织怎么分块（谁归谁管的「块」）                     */
/*    · 汇报关系 —— 人怎么汇报（谁向谁汇报的「线」）                     */
/*  两者在真实组织里经常不一致，画在一张图上只会互相干扰。               */
/* ================================================================== */

const NODE_WIDTH = 190;
const NODE_HEIGHT = 64;
const H_GAP = 26;
const V_GAP = 86;

type DeptNodeData = {
  name: string;
  code: string;
  memberCount: number;
  totalMemberCount: number;
  leaderName?: string;
};

type PersonNodeData = {
  name: string;
  account: string;
  avatar?: string;
  jobTitle?: string;
  isLeader?: boolean;
};

function DeptFlowNode({ data }: NodeProps<Node<DeptNodeData>>) {
  return (
    <div className="rounded-lg border bg-card px-3 py-2 shadow-sm" style={{ width: NODE_WIDTH }}>
      <Handle type="target" position={Position.Top} className="!size-1.5 !border-0 !bg-muted-foreground/40" />
      <div className="truncate text-xs font-semibold">{data.name}</div>
      <div className="mt-0.5 flex items-center justify-between gap-1">
        <span className="truncate font-mono text-[9px] text-muted-foreground">{data.code}</span>
        <Badge variant="outline" className="shrink-0 text-[9px] tabular-nums">
          {data.memberCount}
          {data.totalMemberCount > data.memberCount && (
            <span className="text-muted-foreground">/{data.totalMemberCount}</span>
          )}
        </Badge>
      </div>
      {data.leaderName && (
        <div className="mt-0.5 truncate text-[9px] text-muted-foreground">负责人：{data.leaderName}</div>
      )}
      <Handle type="source" position={Position.Bottom} className="!size-1.5 !border-0 !bg-muted-foreground/40" />
    </div>
  );
}

function PersonFlowNode({ data }: NodeProps<Node<PersonNodeData>>) {
  return (
    <div className="flex items-center gap-2 rounded-lg border bg-card px-2.5 py-2 shadow-sm" style={{ width: NODE_WIDTH }}>
      <Handle type="target" position={Position.Top} className="!size-1.5 !border-0 !bg-muted-foreground/40" />
      <Avatar className="size-7 shrink-0">
        <AvatarImage src={data.avatar} />
        <AvatarFallback className="text-[9px]">{data.name[0]}</AvatarFallback>
      </Avatar>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1">
          <span className="truncate text-xs font-medium">{data.name}</span>
          {data.isLeader && <Badge variant="success" className="shrink-0 text-[8px]">负责人</Badge>}
        </div>
        <div className="truncate text-[9px] text-muted-foreground">{data.jobTitle || data.account}</div>
      </div>
      <Handle type="source" position={Position.Bottom} className="!size-1.5 !border-0 !bg-muted-foreground/40" />
    </div>
  );
}

const nodeTypes = { dept: DeptFlowNode, person: PersonFlowNode };

/**
 * 自底向上的树布局：先算出每棵子树占多宽，再把父节点摆在子树的中点。
 * 这样兄弟节点永不重叠，父节点也总是居中 —— 用 dagre 也能得到类似结果，
 * 但组织树是严格的树（不是有向图），这点布局逻辑不值得再引一个依赖。
 */
function layoutTree<T>(
  roots: { id: string; children: T[] }[],
  getChildren: (node: T) => T[],
  getId: (node: T) => string,
): Map<string, { x: number; y: number }> {
  const positions = new Map<string, { x: number; y: number }>();
  let cursor = 0;

  const walk = (node: { id: string }, children: T[], depth: number): number => {
    if (children.length === 0) {
      const x = cursor * (NODE_WIDTH + H_GAP);
      cursor += 1;
      positions.set(node.id, { x, y: depth * (NODE_HEIGHT + V_GAP) });
      return x;
    }
    const childXs = children.map((child) =>
      walk({ id: getId(child) }, getChildren(child), depth + 1),
    );
    const x = (childXs[0] + childXs[childXs.length - 1]) / 2;
    positions.set(node.id, { x, y: depth * (NODE_HEIGHT + V_GAP) });
    return x;
  };

  roots.forEach((root) => walk(root, root.children, 0));
  return positions;
}

export function OrgChartPanel({ orgId }: { orgId: string }) {
  const treeQuery = useDepartmentTreeQuery(orgId);
  const tree = treeQuery.data ?? [];

  return (
    <Tabs defaultValue="departments">
      <TabsList>
        <TabsTrigger value="departments"><Network className="size-3.5" />部门架构</TabsTrigger>
        <TabsTrigger value="reporting"><GitBranch className="size-3.5" />汇报关系</TabsTrigger>
      </TabsList>

      <TabsContent value="departments" className="mt-3">
        <Card>
          <CardContent className="p-0">
            <DeptChart tree={tree} loading={treeQuery.isLoading} />
          </CardContent>
        </Card>
      </TabsContent>

      <TabsContent value="reporting" className="mt-3">
        <ReportingChart orgId={orgId} tree={tree} />
      </TabsContent>
    </Tabs>
  );
}

function DeptChart({ tree, loading }: { tree: DepartmentNode[]; loading: boolean }) {
  const { nodes, edges } = useMemo(() => {
    if (tree.length === 0) return { nodes: [] as Node[], edges: [] as Edge[] };

    const positions = layoutTree(
      tree.map((n) => ({ id: n.id, children: n.children })),
      (n: DepartmentNode) => n.children,
      (n: DepartmentNode) => n.id,
    );

    const nodes: Node[] = [];
    const edges: Edge[] = [];
    const walk = (node: DepartmentNode, parentId?: string) => {
      const pos = positions.get(node.id) ?? { x: 0, y: 0 };
      nodes.push({
        id: node.id, type: "dept", position: pos,
        data: {
          name: node.name, code: node.code,
          memberCount: node.memberCount, totalMemberCount: node.totalMemberCount,
          leaderName: node.leaderName,
        } satisfies DeptNodeData,
      });
      if (parentId) {
        edges.push({
          id: `${parentId}-${node.id}`, source: parentId, target: node.id,
          type: "smoothstep", style: { strokeWidth: 1.5 },
        });
      }
      node.children.forEach((child) => walk(child, node.id));
    };
    tree.forEach((root) => walk(root));
    return { nodes, edges };
  }, [tree]);

  if (loading) return <div className="py-24 text-center text-xs text-muted-foreground">加载中…</div>;
  if (nodes.length === 0) {
    return <div className="p-6"><EmptyState title="暂无部门" description="先在「组织结构」里建立部门，这里会自动画出架构图" /></div>;
  }

  return (
    <div className="h-[560px] w-full">
      <ReactFlow
        nodes={nodes} edges={edges} nodeTypes={nodeTypes}
        fitView fitViewOptions={{ padding: 0.2 }}
        proOptions={{ hideAttribution: true }}
        nodesDraggable={false} nodesConnectable={false} elementsSelectable={false}
      >
        <Background gap={16} size={1} />
        <Controls showInteractive={false} />
        <MiniMap pannable zoomable className="!bg-muted" />
      </ReactFlow>
    </div>
  );
}

function ReportingChart({ orgId, tree }: { orgId: string; tree: DepartmentNode[] }) {
  const options = flattenDeptOptions(tree);
  const [deptId, setDeptId] = useState<string>(options[0]?.id ?? "");
  const membersQuery = useDepartmentMembersQuery(orgId, deptId || null);
  // 直接用 query 的引用做依赖：`?? []` 每次渲染都会造一个新数组，
  // 会让 useMemo 每次都重算整张图
  const members = useMemo(() => membersQuery.data ?? [], [membersQuery.data]);

  const { nodes, edges } = useMemo(() => {
    if (members.length === 0) return { nodes: [] as Node[], edges: [] as Edge[] };

    // 汇报线构成一片森林：没有上级的人是各自的根
    const byId = new Map(members.map((m) => [m.adminId, m]));
    const childrenOf = new Map<number, typeof members>();
    const roots: typeof members = [];
    members.forEach((m) => {
      const supervisor = m.reportingTo && byId.has(m.reportingTo) ? m.reportingTo : null;
      if (supervisor === null || supervisor === m.adminId) {
        roots.push(m);
        return;
      }
      const list = childrenOf.get(supervisor) ?? [];
      list.push(m);
      childrenOf.set(supervisor, list);
    });

    const positions = new Map<number, { x: number; y: number }>();
    let cursor = 0;
    const walk = (adminId: number, depth: number): number => {
      const children = childrenOf.get(adminId) ?? [];
      if (children.length === 0) {
        const x = cursor * (NODE_WIDTH + H_GAP);
        cursor += 1;
        positions.set(adminId, { x, y: depth * (NODE_HEIGHT + V_GAP) });
        return x;
      }
      const xs = children.map((child) => walk(child.adminId, depth + 1));
      const x = (xs[0] + xs[xs.length - 1]) / 2;
      positions.set(adminId, { x, y: depth * (NODE_HEIGHT + V_GAP) });
      return x;
    };
    roots.forEach((root) => walk(root.adminId, 0));

    const nodes: Node[] = members.map((m) => ({
      id: String(m.adminId), type: "person",
      position: positions.get(m.adminId) ?? { x: 0, y: 0 },
      data: {
        name: m.displayName || m.account, account: m.account,
        avatar: m.avatar, jobTitle: m.jobTitle, isLeader: m.isLeader,
      } satisfies PersonNodeData,
    }));

    const edges: Edge[] = members
      .filter((m) => m.reportingTo && byId.has(m.reportingTo))
      .map((m) => ({
        id: `${m.reportingTo}-${m.adminId}`,
        source: String(m.reportingTo), target: String(m.adminId),
        type: "smoothstep", style: { strokeWidth: 1.5 },
      }));

    return { nodes, edges };
  }, [members]);

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex items-center gap-2">
          <Users className="size-4 text-muted-foreground" />
          <Select value={deptId} onValueChange={setDeptId}>
            <SelectTrigger className="h-8 w-64 text-xs"><SelectValue placeholder="选择部门" /></SelectTrigger>
            <SelectContent>
              {options.map((o) => <SelectItem key={o.id} value={o.id}>{o.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <span className="text-[10px] text-muted-foreground">汇报关系按部门查看</span>
        </div>

        {!deptId ? (
          <EmptyState title="请先选择部门" description="" />
        ) : membersQuery.isLoading ? (
          <div className="py-20 text-center text-xs text-muted-foreground">加载中…</div>
        ) : nodes.length === 0 ? (
          <EmptyState title="该部门暂无成员" description="" />
        ) : (
          <div className="h-[500px] w-full rounded-lg border">
            <ReactFlow
              nodes={nodes} edges={edges} nodeTypes={nodeTypes}
              fitView fitViewOptions={{ padding: 0.2 }}
              proOptions={{ hideAttribution: true }}
              nodesDraggable={false} nodesConnectable={false} elementsSelectable={false}
            >
              <Background gap={16} size={1} />
              <Controls showInteractive={false} />
            </ReactFlow>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
