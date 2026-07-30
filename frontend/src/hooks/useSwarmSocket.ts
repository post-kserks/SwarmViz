import { useEffect, useRef, useCallback } from 'react';
import { useSwarmStore } from '../store/useSwarmStore';
import { WSEventEnvelope, ControlAction } from '../types/swarm';

const BACKOFF_INITIAL_MS = 1000;
const BACKOFF_MAX_MS = 10000;

export function useSwarmSocket() {
  const socketRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const backoffRef = useRef<number>(BACKOFF_INITIAL_MS);

  const {
    setConnectionStatus,
    handleInitState,
    addAgent,
    updateAgentStatus,
    addEdge,
    terminateAgent,
    addClaim,
    releaseClaim,
    addEditEvent,
    setConflict,
    resolveConflict,
    updateFileTreeCount,
    appendLocDelta,
    addToast,
    setLastSeq,
    handleWSEvent,
  } = useSwarmStore();

  const connect = useCallback(() => {
    if (socketRef.current?.readyState === WebSocket.OPEN) return;

    const lastSeq = useSwarmStore.getState().lastSeq;
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    
    const sinceParam = lastSeq > 0 ? `?since=${lastSeq}` : '';
    const wsUrl = `${protocol}//${host}/ws${sinceParam}`;

    setConnectionStatus(backoffRef.current > BACKOFF_INITIAL_MS ? 'reconnecting' : 'connecting');

    const ws = new WebSocket(wsUrl);
    socketRef.current = ws;

    ws.onopen = () => {
      setConnectionStatus('connected');
      backoffRef.current = BACKOFF_INITIAL_MS;
    };

    ws.onmessage = (event) => {
      try {
        const envelope: WSEventEnvelope = JSON.parse(event.data);
        if (envelope.type === 'PING') {
          ws.send(JSON.stringify({ type: 'PONG' }));
          return;
        }
        if (envelope.seq && envelope.seq > 0) {
          setLastSeq(envelope.seq);
        }
        handleWSEvent(envelope);
      } catch (err) {
        console.error('Failed to parse WS message:', err);
      }
    };

    ws.onclose = () => {
      setConnectionStatus('disconnected');
      scheduleReconnect();
    };

    ws.onerror = (err) => {
      console.error('WS Error:', err);
      ws.close();
    };
  }, [setConnectionStatus, setLastSeq, handleWSEvent]);

  const scheduleReconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
    
    const timeout = backoffRef.current;
    backoffRef.current = Math.min(backoffRef.current * 2, BACKOFF_MAX_MS);

    reconnectTimeoutRef.current = setTimeout(() => {
      connect();
    }, timeout);
  }, [connect]);

  const sendControl = useCallback((agentId: string, action: ControlAction) => {
    if (socketRef.current?.readyState === WebSocket.OPEN) {
      socketRef.current.send(
        JSON.stringify({
          type: 'AGENT_CONTROL',
          data: { agent_id: agentId, action },
        })
      );
    } else {
      addToast('error', 'Cannot send control action: WebSocket disconnected');
    }
  }, [addToast]);

  useEffect(() => {
    connect();
    return () => {
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
      if (socketRef.current) socketRef.current.close();
    };
  }, [connect]);

  return { sendControl, sendAgentControl: sendControl };
}
