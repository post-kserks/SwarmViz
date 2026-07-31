import { useEffect, useRef, useCallback } from 'react';
import { useSwarmStore } from '../store/useSwarmStore';
import { WSEventEnvelope, ControlAction } from '../types/swarm';

const BACKOFF_INITIAL_MS = 1000;
const BACKOFF_MAX_MS = 10000;

// basePath is the currently selected project's routing prefix ("" for the
// root project, "/p/{id}" for an extra one — see ProjectSummary). Passing
// null means no project is selected yet, so the socket stays closed.
export function useSwarmSocket(basePath: string | null) {
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
    if (basePath === null) return;
    if (socketRef.current?.readyState === WebSocket.OPEN) return;

    const lastSeq = useSwarmStore.getState().lastSeq;
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;

    const sinceParam = lastSeq > 0 ? `?since=${lastSeq}` : '';
    const wsUrl = `${protocol}//${host}${basePath}/ws${sinceParam}`;

    setConnectionStatus(backoffRef.current > BACKOFF_INITIAL_MS ? 'reconnecting' : 'connecting');

    const ws = new WebSocket(wsUrl);
    socketRef.current = ws;

    ws.onopen = () => {
      setConnectionStatus('connected');
      backoffRef.current = BACKOFF_INITIAL_MS;
    };

    ws.onmessage = (event) => {
      // writePump drains the send queue into ONE text frame, joining the
      // envelopes with '\n' — so a frame holds 1..N of them, not exactly one.
      // Go escapes newlines inside strings, so a raw '\n' is always a
      // separator and never part of an envelope.
      for (const line of (event.data as string).split('\n')) {
        if (!line) continue;
        try {
          const envelope: WSEventEnvelope = JSON.parse(line);
          if (envelope.type === 'PING') {
            ws.send(JSON.stringify({ type: 'PONG' }));
            continue;
          }
          if (envelope.seq && envelope.seq > 0) {
            setLastSeq(envelope.seq);
          }
          handleWSEvent(envelope);
        } catch (err) {
          console.error('Failed to parse WS message:', err);
        }
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
  }, [basePath, setConnectionStatus, setLastSeq, handleWSEvent]);

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
    // A project switch remounts this effect with a new basePath — start its
    // backoff fresh rather than carrying over delay accumulated against the
    // previous project's connection.
    backoffRef.current = BACKOFF_INITIAL_MS;
    connect();
    return () => {
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
      if (socketRef.current) {
        // Detach handlers first: closing here is intentional (unmount or
        // switching projects), so it must not trigger scheduleReconnect,
        // which would otherwise open a stray socket to the old basePath.
        socketRef.current.onclose = null;
        socketRef.current.onerror = null;
        socketRef.current.close();
        socketRef.current = null;
      }
    };
  }, [connect, basePath]);

  return { sendControl, sendAgentControl: sendControl };
}
