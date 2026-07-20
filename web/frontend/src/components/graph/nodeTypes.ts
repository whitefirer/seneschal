import { FlowNode, ForeachGroupNode, ParallelGroupNode, HollowEdge } from './FlowNodes'

// nodeTypes/edgeTypes 必须在组件外部定义，避免每次渲染重新创建。
// 独立成文件：该文件不导出组件，FlowNodes.tsx 保持纯组件导出（react-refresh）。
export const nodeTypes = { flowNode: FlowNode, foreachGroup: ForeachGroupNode, parallelGroup: ParallelGroupNode }
export const edgeTypes = { hollow: HollowEdge }
