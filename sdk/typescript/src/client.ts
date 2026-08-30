import { BusError } from './errors.js'
import type { BusErrorCode } from './errors.js'
import type {
  Agent,
  AgentLifecycle,
  AskHumanInput,
  BusHealth,
  BusMessage,
  BusTask,
  CreateScopeInput,
  CreateScopeResult,
  DeliveryReceipt,
  HumanEscalation,
  InboxReservation,
  RegisterAgentInput,
  RegisterAgentResult,
  SendMessageInput
} from './protocol.js'

interface Success<T> {
  ok: true
  result: T
}

interface Failure {
  ok: false
  error: { code: BusErrorCode; message: string; details?: Record<string, unknown> }
}

async function request<T>(
  address: string,
  token: string | undefined,
  method: string,
  path: string,
  value?: unknown
): Promise<T> {
  let response: Response
  try {
    response = await fetch(`${address}${path}`, {
      method,
      headers: {
        accept: 'application/json',
        ...(value === undefined ? {} : { 'content-type': 'application/json' }),
        ...(token === undefined ? {} : { authorization: `Bearer ${token}` })
      },
      ...(value === undefined ? {} : { body: JSON.stringify(value) })
    })
  } catch (error) {
    const message = error instanceof Error ? error.message : 'Network request failed'
    throw new BusError('INTERNAL', `October Bus request failed: ${message}`)
  }
  const text = await response.text()
  let payload: Success<T> | Failure | T
  try {
    payload = JSON.parse(text) as Success<T> | Failure | T
  } catch {
    throw new BusError('INTERNAL', `October Bus returned a non-JSON response with HTTP ${response.status}`)
  }
  if (response.ok) {
    if (payload && typeof payload === 'object' && 'ok' in payload && payload.ok === true)
      return (payload as Success<T>).result
    return payload as T
  }
  if (
    payload &&
    typeof payload === 'object' &&
    'ok' in payload &&
    payload.ok === false &&
    'error' in payload &&
    payload.error &&
    typeof payload.error === 'object' &&
    'code' in payload.error &&
    'message' in payload.error
  ) {
    const failure = payload as Failure
    throw new BusError(failure.error.code, failure.error.message, failure.error.details)
  }
  throw new BusError('INTERNAL', `October Bus request failed with HTTP ${response.status}`)
}

export class OctoberBusAdminClient {
  constructor(
    readonly address: string,
    private readonly adminToken: string
  ) {}

  health(): Promise<BusHealth> {
    return request(this.address, undefined, 'GET', '/health')
  }

  createScope(input: CreateScopeInput = {}): Promise<CreateScopeResult> {
    return request(this.address, this.adminToken, 'POST', '/v1/scopes', input)
  }
}

export class OctoberBusScopeClient {
  constructor(
    readonly address: string,
    readonly scopeToken: string
  ) {}

  registerAgent(input: RegisterAgentInput): Promise<RegisterAgentResult> {
    return request(this.address, this.scopeToken, 'POST', '/v1/agents', input)
  }

  listAgents(): Promise<Agent[]> {
    return request(this.address, this.scopeToken, 'GET', '/v1/agents')
  }

  async linkAgents(left: string, right: string): Promise<void> {
    await request(this.address, this.scopeToken, 'POST', '/v1/links', { left, right })
  }

  listEscalations(): Promise<HumanEscalation[]> {
    return request(this.address, this.scopeToken, 'GET', '/v1/scope/escalations')
  }

  resolveEscalation(id: string, answer: string): Promise<HumanEscalation> {
    return request(this.address, this.scopeToken, 'POST', `/v1/scope/escalations/${encodeURIComponent(id)}/resolve`, {
      answer
    })
  }
}

export class OctoberBusClient {
  constructor(
    readonly address: string,
    readonly agentToken: string
  ) {}

  heartbeat(lifecycle: AgentLifecycle, ready = true, leaseMs?: number): Promise<Agent> {
    return request(this.address, this.agentToken, 'PATCH', '/v1/me/heartbeat', {
      lifecycle,
      ready,
      ...(leaseMs === undefined ? {} : { leaseMs })
    })
  }

  listPeers(): Promise<Agent[]> {
    return request(this.address, this.agentToken, 'GET', '/v1/peers')
  }

  sendMessage(input: SendMessageInput): Promise<DeliveryReceipt> {
    return request(this.address, this.agentToken, 'POST', '/v1/messages', input)
  }

  receipt(messageId: string): Promise<DeliveryReceipt> {
    return request(this.address, this.agentToken, 'GET', `/v1/messages/${encodeURIComponent(messageId)}`)
  }

  reserveInbox(limit = 50): Promise<InboxReservation | null> {
    return request(this.address, this.agentToken, 'POST', '/v1/inbox/reserve', { limit })
  }

  commitInbox(reservationId: string): Promise<BusMessage[]> {
    return request(
      this.address,
      this.agentToken,
      'POST',
      `/v1/inbox/${encodeURIComponent(reservationId)}/commit`,
      {}
    )
  }

  async releaseInbox(reservationId: string): Promise<void> {
    await request(
      this.address,
      this.agentToken,
      'POST',
      `/v1/inbox/${encodeURIComponent(reservationId)}/release`,
      {}
    )
  }

  async pullInbox(limit = 50): Promise<BusMessage[]> {
    const reservation = await this.reserveInbox(limit)
    return reservation ? this.commitInbox(reservation.id) : []
  }

  async acknowledgeMessages(messageIds: string[]): Promise<number> {
    const result = await request<{ acknowledged: number }>(
      this.address,
      this.agentToken,
      'POST',
      '/v1/messages/ack',
      { messageIds }
    )
    return result.acknowledged
  }

  addTask(description: string, dependencies: string[] = []): Promise<BusTask> {
    return request(this.address, this.agentToken, 'POST', '/v1/tasks', { description, dependencies })
  }

  listTasks(): Promise<BusTask[]> {
    return request(this.address, this.agentToken, 'GET', '/v1/tasks')
  }

  claimTask(taskId: string): Promise<BusTask> {
    return request(this.address, this.agentToken, 'POST', `/v1/tasks/${encodeURIComponent(taskId)}/claim`, {})
  }

  releaseTask(taskId: string): Promise<BusTask> {
    return request(this.address, this.agentToken, 'POST', `/v1/tasks/${encodeURIComponent(taskId)}/release`, {})
  }

  completeTask(taskId: string, note?: string): Promise<BusTask> {
    return request(this.address, this.agentToken, 'POST', `/v1/tasks/${encodeURIComponent(taskId)}/complete`, {
      ...(note === undefined ? {} : { note })
    })
  }

  askHuman(input: AskHumanInput): Promise<HumanEscalation> {
    return request(this.address, this.agentToken, 'POST', '/v1/escalations', input)
  }

  escalation(id: string): Promise<HumanEscalation> {
    return request(this.address, this.agentToken, 'GET', `/v1/escalations/${encodeURIComponent(id)}`)
  }

  mcpEndpoint(): { url: string; headers: Record<string, string> } {
    return { url: `${this.address}/mcp`, headers: { Authorization: `Bearer ${this.agentToken}` } }
  }
}
