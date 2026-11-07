import { apiClient, ownedAuthRequestConfig } from './client'
import { assertAuthSessionCurrent } from './authSession'

export type MCPAccessScope = 'user' | 'admin'

export interface MCPGrant {
  id: string
  client_id: string
  client_name: string
  scope: MCPAccessScope
  read_only: boolean
  created_at: string
  expires_at: string
  last_used_at: string | null
  revoked_at: string | null
  invalidated: boolean
}

export interface MCPAuthorization {
  response_type: string
  client_id: string
  redirect_uri: string
  scope?: string
  state?: string
  code_challenge: string
  code_challenge_method: string
  resource: string
}

export interface MCPConsent {
  client_id: string
  client_name: string
  redirect_uri: string
  resource: string
  scope: 'mcp'
}

export interface MCPAuthorizationDecision {
  authorization: MCPAuthorization
  approved: boolean
  scope: MCPAccessScope
  read_only: boolean
}

export const mcpAPI = {
  async getConnectionConfig(): Promise<{ resource: string }> {
    const owner = ownedAuthRequestConfig()
    const { data } = await apiClient.get<{ resource: string }>('/user/mcp/config', owner)
    assertAuthSessionCurrent(owner.authIdentity.sessionID)
    return data
  },
  async listGrants(): Promise<MCPGrant[]> {
    const owner = ownedAuthRequestConfig()
    const { data } = await apiClient.get<MCPGrant[]>('/user/mcp/grants', owner)
    assertAuthSessionCurrent(owner.authIdentity.sessionID)
    return data
  },
  async revokeGrant(id: string): Promise<void> {
    const owner = ownedAuthRequestConfig()
    await apiClient.delete(`/user/mcp/grants/${encodeURIComponent(id)}`, owner)
    assertAuthSessionCurrent(owner.authIdentity.sessionID)
  },
  async previewAuthorization(authorization: MCPAuthorization): Promise<MCPConsent> {
    const owner = ownedAuthRequestConfig()
    const { data } = await apiClient.get<MCPConsent>('/user/mcp/authorize', { ...owner, params: authorization })
    assertAuthSessionCurrent(owner.authIdentity.sessionID)
    return data
  },
  async authorize(input: MCPAuthorizationDecision): Promise<{ redirect_url: string }> {
    const owner = ownedAuthRequestConfig()
    const { data } = await apiClient.post<{ redirect_url: string }>('/user/mcp/authorize', input, owner)
    assertAuthSessionCurrent(owner.authIdentity.sessionID)
    return data
  }
}
