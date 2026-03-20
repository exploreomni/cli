import { type AuthContext, getAuthHeaders } from '../config/index.js'

export interface APIResponse<T> {
  data?: T
  error?: string
  status: number
}

export class APIClient {
  private baseUrl: string
  private authContext: AuthContext

  constructor(baseUrl: string, authContext: AuthContext) {
    this.baseUrl = baseUrl.replace(/\/$/, '')
    this.authContext = authContext
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<APIResponse<T>> {
    const url = `${this.baseUrl}${path}`
    const headers = getAuthHeaders(this.authContext)

    try {
      const response = await fetch(url, {
        method,
        headers,
        body: body ? JSON.stringify(body) : undefined,
      })

      if (!response.ok) {
        const errorText = await response.text()
        let errorMessage: string
        try {
          const errorJson = JSON.parse(errorText)
          const raw = errorJson.message ?? errorJson.error ?? errorText
          if (typeof raw === 'string') {
            errorMessage = raw
          } else if (
            raw &&
            typeof raw === 'object' &&
            typeof raw.message === 'string'
          ) {
            errorMessage = raw.message
          } else {
            errorMessage = JSON.stringify(raw)
          }
        } catch {
          errorMessage = errorText
        }
        return {
          error: `${response.status}: ${errorMessage}`,
          status: response.status,
        }
      }

      const data = (await response.json()) as T
      return { data, status: response.status }
    } catch (error) {
      const detail =
        error instanceof Error ? error.message : 'Unknown error occurred'
      const message =
        detail === 'fetch failed'
          ? `Could not connect to ${this.baseUrl} — is the server running?`
          : `Request to ${url} failed: ${detail}`
      return { error: message, status: 0 }
    }
  }

  async get<T>(path: string): Promise<APIResponse<T>> {
    return this.request<T>('GET', path)
  }

  async post<T>(path: string, body?: unknown): Promise<APIResponse<T>> {
    return this.request<T>('POST', path, body)
  }

  async put<T>(path: string, body?: unknown): Promise<APIResponse<T>> {
    return this.request<T>('PUT', path, body)
  }

  async delete<T>(path: string): Promise<APIResponse<T>> {
    return this.request<T>('DELETE', path)
  }
}

export const createAPIClient = (
  baseUrl: string,
  authContext: AuthContext
): APIClient => {
  return new APIClient(baseUrl, authContext)
}
