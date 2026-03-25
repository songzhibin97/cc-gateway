import client from './client';
import type { RequestLog, RequestPayload } from '../types';

export interface LogFilter {
  key_id?: string;
  account_id?: string;
  model?: string;
  from?: string;
  to?: string;
  limit?: number;
  offset?: number;
}

export const queryLogs = (filter: LogFilter) =>
  client.get<RequestLog[]>('/admin/logs', { params: filter });
export const getPayload = (logId: number) => client.get<RequestPayload>(`/admin/logs/${logId}/payload`);
