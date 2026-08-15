import { Navigate, useParams } from 'react-router-dom'

// 旧 /dag 路由重定向到统一编辑器
export default function DagRedirect() {
  const { name } = useParams<{ name: string }>()
  return <Navigate to={name ? `/editor/${name}` : '/editor/new'} replace />
}
