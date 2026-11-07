import { apiClient, ownedAuthRequestConfig } from '../client'
import { assertAuthSessionCurrent } from '../authSession'

export interface AccessRule {
  permissions: string[]
  group_ids: number[]
  all_groups: boolean
  include_ungrouped: boolean
  account_types: string[]
  platforms: string[]
}
export interface AccessPolicy { full_access?: boolean; rules: AccessRule[]; global_permissions: string[] }
export interface Permission { id: string; label: string; category: string; scope: 'account' | 'group' | 'global' }
export interface AdminKeyInput {
  confirm_full_access?: boolean
  name: string
  description: string
  enabled: boolean
  expires_at: string | null
  allowed_ips: string[]
  policy: AccessPolicy
}
export interface AdminKey extends AdminKeyInput {
  id: string
  key_prefix: string
  created_by: number
  created_at: string
  updated_at: string
  last_used_at: string | null
  last_used_ip: string
  revoked_at: string | null
}
const base = '/admin/settings/admin-api-keys'
export async function list() {
  const owner = ownedAuthRequestConfig()
  const { data } = await apiClient.get<{ items: AdminKey[]; permissions: Permission[] }>(base, owner)
  assertAuthSessionCurrent(owner.authIdentity.sessionID)
  return data
}
export async function create(input: AdminKeyInput) {
  const owner = ownedAuthRequestConfig()
  const { data } = await apiClient.post<{ key: string; item: AdminKey }>(base, input, owner)
  assertAuthSessionCurrent(owner.authIdentity.sessionID)
  return data
}
export async function update(id: string, input: AdminKeyInput) {
  const owner = ownedAuthRequestConfig()
  const { data } = await apiClient.put<AdminKey>(`${base}/${encodeURIComponent(id)}`, input, owner)
  assertAuthSessionCurrent(owner.authIdentity.sessionID)
  return data
}
export async function rotate(id: string) {
  const owner = ownedAuthRequestConfig()
  const { data } = await apiClient.post<{ key: string; item: AdminKey }>(`${base}/${encodeURIComponent(id)}/rotate`, undefined, owner)
  assertAuthSessionCurrent(owner.authIdentity.sessionID)
  return data
}
export async function revoke(id: string) {
  const owner = ownedAuthRequestConfig()
  const { data } = await apiClient.delete<{ message: string }>(`${base}/${encodeURIComponent(id)}`, owner)
  assertAuthSessionCurrent(owner.authIdentity.sessionID)
  return data
}
export async function deleteRecord(id: string) {
  const owner = ownedAuthRequestConfig()
  const { data } = await apiClient.delete<{ message: string }>(`${base}/${encodeURIComponent(id)}/record`, owner)
  assertAuthSessionCurrent(owner.authIdentity.sessionID)
  return data
}
