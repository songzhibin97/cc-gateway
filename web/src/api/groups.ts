import client from './client';
import type { KeyGroup } from '../types';

export const listGroups = () => client.get<KeyGroup[]>('/admin/groups');
export const getGroup = (id: string) => client.get<KeyGroup>(`/admin/groups/${id}`);
export const createGroup = (data: Partial<KeyGroup>) => client.post<KeyGroup>('/admin/groups', data);
export const updateGroup = (id: string, data: Partial<KeyGroup>) =>
  client.put<KeyGroup>(`/admin/groups/${id}`, data);
export const deleteGroup = (id: string) => client.delete(`/admin/groups/${id}`);
