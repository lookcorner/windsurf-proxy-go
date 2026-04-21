import { create } from 'zustand';
import type { Stats, Instance, ApiKey, AppConfig, RequestRecord } from '@/lib/api';
import * as api from '@/lib/api';

export type Page = 'dashboard' | 'accounts' | 'apikeys' | 'settings' | 'requests' | 'models_view';

interface LogEntry {
  time: string;
  level: string;
  message: string;
}

interface AppState {
  // Navigation
  currentPage: Page;
  setPage: (page: Page) => void;

  // Stats
  stats: Stats | null;
  loadStats: () => Promise<void>;

  // Instances
  instances: Instance[];
  loadInstances: () => Promise<void>;

  // API Keys
  apiKeys: ApiKey[];
  loadKeys: () => Promise<void>;

  // Config
  config: AppConfig | null;
  loadConfig: () => Promise<void>;

  // Models
  models: string[];
  loadModels: () => Promise<void>;

  // Request History
  requestHistory: RequestRecord[];
  loadRequestHistory: () => Promise<void>;

  // Logs
  logs: LogEntry[];
  addLog: (entry: LogEntry) => void;

  // Loading state
  loading: boolean;
}

export const useAppStore = create<AppState>((set) => ({
  currentPage: 'dashboard',
  setPage: (page) => set({ currentPage: page }),

  stats: null,
  loadStats: async () => {
    try {
      const stats = await api.fetchStats();
      set({ stats });
    } catch (e) {
      console.error('Failed to load stats:', e);
    }
  },

  instances: [],
  loadInstances: async () => {
    try {
      const instances = await api.fetchInstances();
      set({ instances });
    } catch (e) {
      console.error('Failed to load instances:', e);
    }
  },

  apiKeys: [],
  loadKeys: async () => {
    try {
      const apiKeys = await api.fetchKeys();
      set({ apiKeys });
    } catch (e) {
      console.error('Failed to load keys:', e);
    }
  },

  config: null,
  loadConfig: async () => {
    try {
      const config = await api.fetchConfig();
      set({ config });
    } catch (e) {
      console.error('Failed to load config:', e);
    }
  },

  models: [],
  loadModels: async () => {
    try {
      const models = await api.fetchModels();
      set({ models });
    } catch (e) {
      console.error('Failed to load models:', e);
    }
  },

  requestHistory: [],
  loadRequestHistory: async () => {
    try {
      const requestHistory = await api.fetchRequests();
      set({ requestHistory });
    } catch (e) {
      console.error('Failed to load request history:', e);
    }
  },

  logs: [],
  addLog: (entry) =>
    set((state) => ({
      logs: [...state.logs.slice(-200), entry],
    })),

  loading: false,
}));
