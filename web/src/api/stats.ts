import client from './client';
import type { CostStats } from '../types';

export interface StatsFilter {
  group_by?: 'key' | 'account' | 'model';
  from?: string;
  to?: string;
}

export const queryCostStats = (filter: StatsFilter) =>
  client.get<CostStats[]>('/admin/stats/cost', { params: filter });
export const triggerCleanup = () => client.post<{ deleted: number; status: string }>('/admin/cleanup');
export const healthCheck = () => client.get<{ status: string }>('/admin/health');
