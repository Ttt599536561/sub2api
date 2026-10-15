import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import axios from 'axios'
import { groupsAPI } from '@/api/admin/groups'
import { apiClient } from '@/api/client'
import { getGroupRateMultipliers, clearGroupRateMultipliers, batchSetGroupRateMultipliers, getGroupRPMOverrides, clearGroupRPMOverrides, batchSetGroupRPMOverrides } from '@/api/admin/groups'
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const original = apiClient.defaults.adapter
const flows = [
 () => groupsAPI.list(), () => groupsAPI.getAll(), () => groupsAPI.getByPlatform('openai'), () => groupsAPI.getAllIncludingInactive(),
 () => groupsAPI.getLiveCapability(), () => groupsAPI.getById(7), () => groupsAPI.getModelAllowlistCandidates(7),
 () => groupsAPI.create({name:'audit'} as any), () => groupsAPI.duplicate(7), () => groupsAPI.update(7,{}), () => groupsAPI.delete(7),
 () => groupsAPI.toggleStatus(7,'active'), () => groupsAPI.getStats(7), () => groupsAPI.getGroupApiKeys(7),
 () => groupsAPI.listCompositeRoutes(7), () => groupsAPI.createCompositeRoute(7,{} as any), () => groupsAPI.updateCompositeRoute(7,8,{} as any),
 () => groupsAPI.deleteCompositeRoute(7,8), () => groupsAPI.previewCompositeRoute(7,{} as any), () => groupsAPI.updateSortOrder([{id:7,sort_order:1}]),
 () => groupsAPI.getUsageSummary(), () => groupsAPI.getCapacitySummary(),

 () => getGroupRateMultipliers(7), () => clearGroupRateMultipliers(7), () => batchSetGroupRateMultipliers(7, [{user_id:2,rate_multiplier:2}]),
 () => getGroupRPMOverrides(7), () => clearGroupRPMOverrides(7), () => batchSetGroupRPMOverrides(7, [{user_id:2,rpm_override:2}])
]
function login(session:string) { localStorage.setItem('auth_user',JSON.stringify({id:7,role:'admin'})); localStorage.setItem('auth_session_id',session); localStorage.setItem('auth_token',session) }
beforeEach(() => { localStorage.clear(); login('A') })
afterEach(() => { apiClient.defaults.adapter=original; localStorage.clear() })
describe.each(flows)('group bulk API owner %s', start => {
 it('never dispatches an old action under a new login',async()=>{
  const adapter=vi.fn(async(config:any)=>({data:[],status:200,statusText:'OK',headers:{},config})); apiClient.defaults.adapter=adapter
  const pending=start(); const check=expect(pending).rejects.toSatisfy(axios.isCancel); login('B'); await check; expect(adapter).not.toHaveBeenCalled()
 })
 it('does not return data from a replaced login',async()=>{
  let finish!:()=>void; let started!:()=>void; const reached=new Promise<void>(r=>{started=r})
  apiClient.defaults.adapter=config=>{started(); return new Promise(r=>{finish=()=>r({data:[],status:200,statusText:'OK',headers:{},config})})}
  const pending=start(); const check=expect(pending).rejects.toMatchObject({code:'AUTH_SESSION_CHANGED'}); await reached; login('B'); finish(); await check
 })
})
