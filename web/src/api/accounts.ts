import client from './client';
import type { Account } from '../types';

export const listAccounts = () => client.get<Account[]>('/admin/accounts');
export const getAccount = (id: string) => client.get<Account>(`/admin/accounts/${id}`);
export const createAccount = (data: Partial<Account>) => client.post<Account>('/admin/accounts', data);
export const updateAccount = (id: string, data: Partial<Account>) =>
  client.put<Account>(`/admin/accounts/${id}`, data);
export const deleteAccount = (id: string) => client.delete(`/admin/accounts/${id}`);
export const updateAccountStatus = (id: string, status: string) =>
  client.put(`/admin/accounts/${id}/status`, { status });
export const resetBreaker = (id: string) => client.post(`/admin/accounts/${id}/reset-breaker`);
