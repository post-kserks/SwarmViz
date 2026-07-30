import React from 'react';
import { MainLayout } from './components/layout/MainLayout';
import { useSwarmSocket } from './hooks/useSwarmSocket';

export const App: React.FC = () => {
  // Initialize WebSocket connection to backend AgentEventHub
  useSwarmSocket();

  return <MainLayout />;
};

export default App;
