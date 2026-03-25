export interface Account {
  id: string;
  name: string;
  provider: 'anthropic' | 'openai' | 'gemini' | 'custom_openai' | 'custom_anthropic';
  api_key: string;
  base_url: string;
  proxy_url: string;
  user_agent: string;
  status: 'enabled' | 'disabled';
  allowed_models: string[];
  model_aliases: Record<string, string> | null;
  max_concurrent: number;
  circuit_breaker: {
    failure_threshold: number;
    success_threshold: number;
    open_duration: string;
  };
  extra: Record<string, any> | null;
  breaker_state?: string;
}

export interface KeyGroup {
  id: string;
  name: string;
  account_ids: string[];
  allowed_models: string[] | null;
  balancer: string;
}

export interface APIKey {
  id: string;
  key_hint: string;
  group_id: string;
  status: 'enabled' | 'disabled';
  allowed_models: string[];
  max_concurrent: number;
  max_input_tokens_monthly: number;
  max_output_tokens_monthly: number;
  used_input_tokens: number;
  used_output_tokens: number;
  created_at: string;
}

export interface CreateKeyResponse {
  key: APIKey;
  raw_key: string;
}

export interface RequestLog {
  ID: number;
  CreatedAt: string;
  KeyID: string;
  KeyHint: string;
  AccountID: string;
  AccountName: string;
  Provider: string;
  ModelRequested: string;
  ModelActual: string;
  InputTokens: number;
  OutputTokens: number;
  ThinkingTokens: number;
  CacheReadTokens: number;
  CacheWriteTokens: number;
  CostUSD: number;
  LatencyMs: number;
  StopReason: string;
  Error: string;
  StatusCode: number;
}

export interface RequestPayload {
  LogID: number;
  RequestBody: string;
  ResponseBody: string;
}

export interface CostStats {
  group_by: string;
  group_value: string;
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
  request_count: number;
}
