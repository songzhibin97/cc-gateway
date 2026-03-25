import client from './client';
import type { APIKey, CreateKeyResponse } from '../types';

export const listKeys = () => client.get<APIKey[]>('/admin/keys');
export const getKey = (id: string) => client.get<APIKey>(`/admin/keys/${id}`);
export const createKey = (data: Partial<APIKey>) => client.post<CreateKeyResponse>('/admin/keys', data);
export const updateKey = (id: string, data: Partial<APIKey>) =>
  client.put<APIKey>(`/admin/keys/${id}`, data);
export const deleteKey = (id: string) => client.delete(`/admin/keys/${id}`);
export const updateKeyStatus = (id: string, status: string) =>
  client.put(`/admin/keys/${id}/status`, { status });
export const rotateKey = (id: string) => client.post<CreateKeyResponse>(`/admin/keys/${id}/rotate`);
