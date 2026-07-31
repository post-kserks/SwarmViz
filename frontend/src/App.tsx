import React from 'react';
import { MainLayout } from './components/layout/MainLayout';
import { useSwarmSocket } from './hooks/useSwarmSocket';
import { useProjectDiscovery } from './hooks/useProjectDiscovery';
import { useCurrentBasePath } from './store/useSwarmStore';

export const App: React.FC = () => {
  // Discover watchable projects and pick the initial one.
  useProjectDiscovery();

  // Initialize WebSocket connection to backend AgentEventHub, scoped to
  // whichever project is currently selected.
  useSwarmSocket(useCurrentBasePath());

  return <MainLayout />;
};

export default App;
