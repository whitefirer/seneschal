import { useEffect, useRef, useCallback, useState } from 'react'

export interface ProgressEvent {
  type: string
  executionId: string
  workflowName: string
  workflowFile?: string
  stepId?: string
  stepName?: string
  name?: string
  action?: string
  status?: string
  output?: string
  error?: string
  duration?: string
  timestamp: string
  time?: string
  // 后端格式化的日志消息
  logMessage?: string
  logLevel?: string
  // Condition 特有字段
  conditionResult?: boolean
}

interface UseWebSocketOptions {
  onMessage?: (event: ProgressEvent) => void
  onError?: (error: Event) => void
  onOpen?: () => void
  onClose?: () => void
}

export function useWebSocket(options: UseWebSocketOptions = {}) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const messageHandlerRef = useRef(options.onMessage)
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const isManualDisconnect = useRef(false)

  // Keep callback ref up to date
  useEffect(() => {
    messageHandlerRef.current = options.onMessage
  }, [options.onMessage])

  const disconnect = useCallback(() => {
    isManualDisconnect.current = true
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }
    if (wsRef.current) {
      wsRef.current.close(1000, 'Client disconnect')
      wsRef.current = null
    }
    setConnected(false)
  }, [])

  const connect = useCallback(() => {
    isManualDisconnect.current = false
    
    // Clear any pending reconnect
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/ws`
    
    try {
      const ws = new WebSocket(wsUrl)
      wsRef.current = ws

      ws.onopen = () => {
        setConnected(true)
        setError(null)
        options.onOpen?.()
      }

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as ProgressEvent
          messageHandlerRef.current?.(data)
        } catch (e) {
          console.error('Failed to parse WebSocket message:', e)
        }
      }

      ws.onerror = (e) => {
        setError('WebSocket connection error')
        options.onError?.(e)
      }

      ws.onclose = () => {
        setConnected(false)
        options.onClose?.()
        
        // Only reconnect if not manually disconnected
        if (!isManualDisconnect.current && wsRef.current !== null) {
          reconnectTimeoutRef.current = setTimeout(() => {
            connect()
          }, 3000)
        }
      }
    } catch (e) {
      setError('Failed to connect: ' + (e as Error).message)
    }
  }, [options.onOpen, options.onError, options.onClose])

  const subscribe = useCallback((executionId: string) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({
        type: 'subscribe',
        data: { executionId }
      }))
    }
  }, [])

  const unsubscribe = useCallback((executionId: string) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({
        type: 'unsubscribe',
        data: { executionId }
      }))
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      disconnect()
    }
  }, []) // Only connect once on mount

  return {
    connected,
    error,
    connect,
    disconnect,
    subscribe,
    unsubscribe,
  }
}
