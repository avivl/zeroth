/* eslint-disable */
/* tslint:disable */
// @ts-nocheck
/*
 * ---------------------------------------------------------------
 * ## THIS FILE WAS GENERATED VIA SWAGGER-TYPESCRIPT-API        ##
 * ##                                                           ##
 * ## AUTHOR: acacode                                           ##
 * ## SOURCE: https://github.com/acacode/swagger-typescript-api ##
 * ---------------------------------------------------------------
 */

export enum AuditResourceType {
  Run = "run",
  Plan = "plan",
  Agent = "agent",
  Memory = "memory",
  Checkpoint = "checkpoint",
}

export enum MemoryProposalStatus {
  Pending = "pending",
  Accepted = "accepted",
  Rejected = "rejected",
}

export enum MemoryKind {
  Session = "session",
  Agent = "agent",
  Operator = "operator",
}

export enum ApprovalStatus {
  Pending = "pending",
  Approved = "approved",
  ChangesRequested = "changes_requested",
}

export enum AgentStatus {
  Ready = "ready",
  Busy = "busy",
  Disabled = "disabled",
}

/** Plan lifecycle. Apply is a daemon transition after approve, not a client operation. */
export enum PlanStatus {
  Draft = "draft",
  CrossExam = "cross_exam",
  PendingApproval = "pending_approval",
  Approved = "approved",
  ChangesRequested = "changes_requested",
  Applying = "applying",
  Applied = "applied",
  Abandoned = "abandoned",
}

/** Lifecycle of a run. Unknown values must be treated as non-terminal. */
export enum RunStatus {
  Pending = "pending",
  Running = "running",
  WaitingApproval = "waiting_approval",
  Backgrounded = "backgrounded",
  Completed = "completed",
  Failed = "failed",
  Cancelled = "cancelled",
}

export interface Health {
  status: "ok";
}

export interface Error {
  /**
   * Machine-readable error code.
   * @minLength 1
   */
  code: string;
  /**
   * Human-readable explanation.
   * @minLength 1
   */
  message: string;
}

/**
 * Opaque run id. Not interchangeable with other id kinds.
 * @minLength 1
 */
export type RunID = string;

/**
 * Opaque plan id. Not interchangeable with other id kinds.
 * @minLength 1
 */
export type PlanID = string;

/**
 * Opaque agent id. Not interchangeable with other id kinds.
 * @minLength 1
 */
export type AgentID = string;

/**
 * Opaque approval id. Not interchangeable with other id kinds.
 * @minLength 1
 */
export type ApprovalID = string;

/**
 * Opaque memory entry id. Not interchangeable with other id kinds.
 * @minLength 1
 */
export type MemoryID = string;

/**
 * Opaque memory proposal id. Not interchangeable with other id kinds.
 * @minLength 1
 */
export type MemoryProposalID = string;

/**
 * Opaque audit record id. Not interchangeable with other id kinds.
 * @minLength 1
 */
export type AuditID = string;

/**
 * Opaque checkpoint id. Not interchangeable with other id kinds.
 * @minLength 1
 */
export type CheckpointID = string;

export interface Run {
  /** Opaque run id. Not interchangeable with other id kinds. */
  id: RunID;
  /** Opaque agent id. Not interchangeable with other id kinds. */
  agent_id: AgentID;
  /** Opaque plan id. Not interchangeable with other id kinds. */
  plan_id?: PlanID;
  /** Lifecycle of a run. Unknown values must be treated as non-terminal. */
  status: RunStatus;
  /** @minLength 1 */
  goal: string;
  /**
   * Optional tracker issue id (for example a Linear issue id). This API does not proxy the tracker.
   * @minLength 1
   */
  issue_ref?: string;
  /**
   * Earned autonomy tier for this run. The kernel interprets the value; unknown tiers deny by default.
   * @minLength 1
   */
  autonomy_tier?: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  created_at: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  updated_at: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  finished_at?: string;
}

export interface RunList {
  items: Run[];
  /**
   * Omitted when there is no next page.
   * @minLength 1
   */
  next_cursor?: string;
}

export interface CreateRunRequest {
  /** Opaque agent id. Not interchangeable with other id kinds. */
  agent_id: AgentID;
  /** @minLength 1 */
  goal: string;
  /**
   * Optional tracker issue id to attach to the run.
   * @minLength 1
   */
  issue_ref?: string;
  /** If true, start the run backgrounded. */
  background?: boolean;
}

export interface SteerRequest {
  /**
   * Operator guidance to inject into the live run.
   * @minLength 1
   */
  message: string;
}

export interface RunEvent {
  /** @minLength 1 */
  id: string;
  /** Opaque run id. Not interchangeable with other id kinds. */
  run_id: RunID;
  /**
   * Event kind. Clients must tolerate unknown types; the log grows with the kernel.
   * @minLength 1
   */
  type: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  created_at: string;
  /** Opaque plan id. Not interchangeable with other id kinds. */
  plan_id?: PlanID;
  /** Human-readable summary of the event, when one exists. */
  message?: string;
}

export interface RunEventList {
  items: RunEvent[];
}

export interface Plan {
  /** Opaque plan id. Not interchangeable with other id kinds. */
  id: PlanID;
  /** Opaque run id. Not interchangeable with other id kinds. */
  run_id: RunID;
  /** Set when this plan was created by branch. */
  parent_plan_id?: PlanID;
  /** Plan lifecycle. Apply is a daemon transition after approve, not a client operation. */
  status: PlanStatus;
  /** @minLength 1 */
  summary: string;
  steps?: PlanStep[];
  /** Structured challenge of the draft, when cross-exam has run. */
  cross_exam?: string;
  /** Latest operator comment from approve or request-changes. */
  review_comment?: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  created_at: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  updated_at: string;
}

export interface PlanStep {
  /** @minLength 1 */
  title: string;
  detail?: string;
  /**
   * Step kind (open string so the kernel can add kinds without a spec bump).
   * @minLength 1
   */
  kind?: string;
}

export interface ApproveRequest {
  /** @minLength 1 */
  comment?: string;
}

export interface RequestChangesRequest {
  /** @minLength 1 */
  comment: string;
}

export interface BranchPlanRequest {
  /**
   * Why this branch exists.
   * @minLength 1
   */
  note?: string;
}

export interface Agent {
  /** Opaque agent id. Not interchangeable with other id kinds. */
  id: AgentID;
  /** @minLength 1 */
  name: string;
  /**
   * Harness driver name. Stage 1 is claudecode.
   * @minLength 1
   */
  harness: string;
  status: AgentStatus;
  config?: AgentConfig;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  created_at: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  updated_at: string;
}

export interface AgentConfig {
  /** @minLength 1 */
  model?: string;
  instructions?: string;
}

export interface AgentPatch {
  /** @minLength 1 */
  name?: string;
  status?: AgentStatus;
  config?: AgentConfig;
}

export interface AgentList {
  items: Agent[];
  /** @minLength 1 */
  next_cursor?: string;
}

export interface Approval {
  /** Opaque approval id. Not interchangeable with other id kinds. */
  id: ApprovalID;
  /** Stage 1 approvals are plan approvals. */
  kind: "plan";
  status: ApprovalStatus;
  /** Opaque plan id. Not interchangeable with other id kinds. */
  plan_id: PlanID;
  /** Opaque run id. Not interchangeable with other id kinds. */
  run_id: RunID;
  summary?: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  created_at: string;
}

export interface ApprovalList {
  items: Approval[];
  /** @minLength 1 */
  next_cursor?: string;
}

export interface MemoryEntry {
  /** Opaque memory entry id. Not interchangeable with other id kinds. */
  id: MemoryID;
  kind: MemoryKind;
  /**
   * Run id when kind is session, agent id when kind is agent.
   * @minLength 1
   */
  ref_id?: string;
  /** @minLength 1 */
  content: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  created_at: string;
}

export interface MemoryList {
  items: MemoryEntry[];
  /** @minLength 1 */
  next_cursor?: string;
}

export interface CreateMemoryRequest {
  kind: MemoryKind;
  /**
   * Required when kind is session or agent.
   * @minLength 1
   */
  ref_id?: string;
  /** @minLength 1 */
  content: string;
}

export interface MemoryProposal {
  /** Opaque memory proposal id. Not interchangeable with other id kinds. */
  id: MemoryProposalID;
  kind: MemoryKind;
  /** @minLength 1 */
  ref_id?: string;
  /** Run that proposed this memory, when it came from a run. */
  run_id?: RunID;
  /** @minLength 1 */
  content: string;
  status: MemoryProposalStatus;
  /** Set after accept, when an entry was written. */
  memory_id?: MemoryID;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  created_at: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  reviewed_at?: string;
}

export interface MemoryProposalList {
  items: MemoryProposal[];
  /** @minLength 1 */
  next_cursor?: string;
}

export interface AuditRecord {
  /** Opaque audit record id. Not interchangeable with other id kinds. */
  id: AuditID;
  /**
   * What happened (open string; clients must tolerate unknown actions).
   * @minLength 1
   */
  action: string;
  resource_type: AuditResourceType;
  /** @minLength 1 */
  resource_id: string;
  /**
   * Local operator identity used when signing.
   * @minLength 1
   */
  actor?: string;
  /**
   * secp256k1 Schnorr signature, hex-encoded (ADR-Z-0007). Not a credential.
   * @minLength 1
   */
  signature: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  created_at: string;
}

export interface AuditList {
  items: AuditRecord[];
  /** @minLength 1 */
  next_cursor?: string;
}

export interface AuditVerification {
  /** Opaque audit record id. Not interchangeable with other id kinds. */
  id: AuditID;
  valid: boolean;
  /** Set when valid is false. */
  reason?: string;
}

export interface Checkpoint {
  /** Opaque checkpoint id. Not interchangeable with other id kinds. */
  id: CheckpointID;
  /** Opaque run id. Not interchangeable with other id kinds. */
  run_id: RunID;
  label?: string;
  /**
   * RFC 3339 timestamp in UTC.
   * @format date-time
   */
  created_at: string;
}

export interface CheckpointList {
  items: Checkpoint[];
  /** @minLength 1 */
  next_cursor?: string;
}

export type QueryParamsType = Record<string | number, any>;
export type ResponseFormat = keyof Omit<Body, "body" | "bodyUsed">;

export interface FullRequestParams extends Omit<RequestInit, "body"> {
  /** set parameter to `true` for call `securityWorker` for this request */
  secure?: boolean;
  /** request path */
  path: string;
  /** content type of request body */
  type?: ContentType;
  /** query params */
  query?: QueryParamsType;
  /** format of response (i.e. response.json() -> format: "json") */
  format?: ResponseFormat;
  /** request body */
  body?: unknown;
  /** base url */
  baseUrl?: string;
  /** request cancellation token */
  cancelToken?: CancelToken;
}

export type RequestParams = Omit<
  FullRequestParams,
  "body" | "method" | "query" | "path"
>;

export interface ApiConfig<SecurityDataType = unknown> {
  baseUrl?: string;
  baseApiParams?: Omit<RequestParams, "baseUrl" | "cancelToken" | "signal">;
  securityWorker?: (
    securityData: SecurityDataType | null,
  ) => Promise<RequestParams | void> | RequestParams | void;
  customFetch?: typeof fetch;
}

export interface HttpResponse<D extends unknown, E extends unknown = unknown>
  extends Response {
  data: D;
  error: E;
}

type CancelToken = Symbol | string | number;

export enum ContentType {
  Json = "application/json",
  JsonApi = "application/vnd.api+json",
  FormData = "multipart/form-data",
  UrlEncoded = "application/x-www-form-urlencoded",
  Text = "text/plain",
}

export class HttpClient<SecurityDataType = unknown> {
  public baseUrl: string = "/";
  private securityData: SecurityDataType | null = null;
  private securityWorker?: ApiConfig<SecurityDataType>["securityWorker"];
  private abortControllers = new Map<CancelToken, AbortController>();
  private customFetch = (...fetchParams: Parameters<typeof fetch>) =>
    fetch(...fetchParams);

  private baseApiParams: RequestParams = {
    credentials: "same-origin",
    headers: {},
    redirect: "follow",
    referrerPolicy: "no-referrer",
  };

  constructor(apiConfig: ApiConfig<SecurityDataType> = {}) {
    Object.assign(this, apiConfig);
  }

  public setSecurityData = (data: SecurityDataType | null) => {
    this.securityData = data;
  };

  protected encodeQueryParam(key: string, value: any) {
    const encodedKey = encodeURIComponent(key);
    return `${encodedKey}=${encodeURIComponent(typeof value === "number" ? value : `${value}`)}`;
  }

  protected addQueryParam(query: QueryParamsType, key: string) {
    return this.encodeQueryParam(key, query[key]);
  }

  protected addArrayQueryParam(query: QueryParamsType, key: string) {
    const value = query[key];
    return value.map((v: any) => this.encodeQueryParam(key, v)).join("&");
  }

  protected toQueryString(rawQuery?: QueryParamsType): string {
    const query = rawQuery || {};
    const keys = Object.keys(query).filter(
      (key) => "undefined" !== typeof query[key],
    );
    return keys
      .map((key) =>
        Array.isArray(query[key])
          ? this.addArrayQueryParam(query, key)
          : this.addQueryParam(query, key),
      )
      .join("&");
  }

  protected addQueryParams(rawQuery?: QueryParamsType): string {
    const queryString = this.toQueryString(rawQuery);
    return queryString ? `?${queryString}` : "";
  }

  private contentFormatters: Record<ContentType, (input: any) => any> = {
    [ContentType.Json]: (input: any) =>
      input !== null && (typeof input === "object" || typeof input === "string")
        ? JSON.stringify(input)
        : input,
    [ContentType.JsonApi]: (input: any) =>
      input !== null && (typeof input === "object" || typeof input === "string")
        ? JSON.stringify(input)
        : input,
    [ContentType.Text]: (input: any) =>
      input !== null && typeof input !== "string"
        ? JSON.stringify(input)
        : input,
    [ContentType.FormData]: (input: any) => {
      if (input instanceof FormData) {
        return input;
      }

      return Object.keys(input || {}).reduce((formData, key) => {
        const property = input[key];
        formData.append(
          key,
          property instanceof Blob
            ? property
            : typeof property === "object" && property !== null
              ? JSON.stringify(property)
              : `${property}`,
        );
        return formData;
      }, new FormData());
    },
    [ContentType.UrlEncoded]: (input: any) => this.toQueryString(input),
  };

  protected mergeRequestParams(
    params1: RequestParams,
    params2?: RequestParams,
  ): RequestParams {
    return {
      ...this.baseApiParams,
      ...params1,
      ...(params2 || {}),
      headers: {
        ...(this.baseApiParams.headers || {}),
        ...(params1.headers || {}),
        ...((params2 && params2.headers) || {}),
      },
    };
  }

  protected createAbortSignal = (
    cancelToken: CancelToken,
  ): AbortSignal | undefined => {
    if (this.abortControllers.has(cancelToken)) {
      const abortController = this.abortControllers.get(cancelToken);
      if (abortController) {
        return abortController.signal;
      }
      return void 0;
    }

    const abortController = new AbortController();
    this.abortControllers.set(cancelToken, abortController);
    return abortController.signal;
  };

  public abortRequest = (cancelToken: CancelToken) => {
    const abortController = this.abortControllers.get(cancelToken);

    if (abortController) {
      abortController.abort();
      this.abortControllers.delete(cancelToken);
    }
  };

  public request = async <T = any, E = any>({
    body,
    secure,
    path,
    type,
    query,
    format,
    baseUrl,
    cancelToken,
    ...params
  }: FullRequestParams): Promise<HttpResponse<T, E>> => {
    const secureParams =
      ((typeof secure === "boolean" ? secure : this.baseApiParams.secure) &&
        this.securityWorker &&
        (await this.securityWorker(this.securityData))) ||
      {};
    const requestParams = this.mergeRequestParams(params, secureParams);
    const queryString = query && this.toQueryString(query);
    const payloadFormatter = this.contentFormatters[type || ContentType.Json];
    const responseFormat = format || requestParams.format;

    return this.customFetch(
      `${baseUrl || this.baseUrl || ""}${path}${queryString ? `?${queryString}` : ""}`,
      {
        ...requestParams,
        headers: {
          ...(requestParams.headers || {}),
          ...(type && type !== ContentType.FormData
            ? { "Content-Type": type }
            : {}),
        },
        signal:
          (cancelToken
            ? this.createAbortSignal(cancelToken)
            : requestParams.signal) || null,
        body:
          typeof body === "undefined" || body === null
            ? null
            : payloadFormatter(body),
      },
    ).then(async (response) => {
      const r = response as HttpResponse<T, E>;
      r.data = null as unknown as T;
      r.error = null as unknown as E;

      const responseToParse = responseFormat ? response.clone() : response;
      const data = !responseFormat
        ? r
        : await responseToParse[responseFormat]()
            .then((data) => {
              if (r.ok) {
                r.data = data;
              } else {
                r.error = data;
              }
              return r;
            })
            .catch((e) => {
              r.error = e;
              return r;
            });

      if (cancelToken) {
        this.abortControllers.delete(cancelToken);
      }

      if (!response.ok) throw data;
      return data;
    });
  };
}

/**
 * @title Zeroth API
 * @version 0.0.0
 * @license MIT
 * @baseUrl /
 * @contact (https://github.com/avivl/zeroth)
 *
 * Local control-plane API for Zeroth. Stage 1 is single-player and binds
 * locally. This spec is the canonical contract; Go stubs and the TypeScript
 * client are generated from it, not hand-written.
 *
 * A flow that cannot be expressed through this API ships on neither the
 * web UI nor the CLI. That is what keeps a later Slack surface from becoming
 * a second implementation of everything.
 *
 * Apply is not a client operation. The human gate is approve (or request
 * changes, or branch). The daemon applies an approved plan; clients watch
 * the run and the plan.
 */
export class Api<
  SecurityDataType extends unknown,
> extends HttpClient<SecurityDataType> {
  health = {
    /**
     * No description
     *
     * @tags health
     * @name Health
     * @summary Liveness
     * @request GET:/health
     */
    health: (params: RequestParams = {}) =>
      this.request<Health, any>({
        path: `/health`,
        method: "GET",
        format: "json",
        ...params,
      }),
  };
  runs = {
    /**
     * No description
     *
     * @tags runs
     * @name ListRuns
     * @summary List runs
     * @request GET:/runs
     */
    listRuns: (
      query?: {
        /**
         * Opaque page cursor from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
        /**
         * Page size. The daemon defaults this when omitted.
         * @min 1
         * @max 200
         */
        limit?: number;
        /** If set, only runs in this status. */
        status?: RunStatus;
        /** If set, only runs for this agent. */
        agent_id?: AgentID;
      },
      params: RequestParams = {},
    ) =>
      this.request<RunList, any>({
        path: `/runs`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags runs
     * @name CreateRun
     * @summary Start a run
     * @request POST:/runs
     */
    createRun: (data: CreateRunRequest, params: RequestParams = {}) =>
      this.request<Run, Error>({
        path: `/runs`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags runs
     * @name GetRun
     * @summary Get a run
     * @request GET:/runs/{id}
     */
    getRun: (id: RunID, params: RequestParams = {}) =>
      this.request<Run, Error>({
        path: `/runs/${id}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The event log is the source of truth for a run, not chat residue. Query parameter n selects how many recent events to replay. A WebSocket upgrade on this path replays those events, then tails live. Each WebSocket message is a JSON RunEvent. Without Upgrade, this GET returns the same replay window as JSON so the CLI and tests can read the log without a socket.
     *
     * @tags runs
     * @name GetRunEvents
     * @summary Replay recent events, then live tail
     * @request GET:/runs/{id}/events
     */
    getRunEvents: (
      id: RunID,
      query?: {
        /**
         * Number of recent events to replay. The daemon defaults this when omitted.
         * @min 1
         * @max 1000
         */
        n?: number;
      },
      params: RequestParams = {},
    ) =>
      this.request<RunEventList, Error>({
        path: `/runs/${id}/events`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description Inject operator guidance into a live run. Does not skip plan-then-apply.
     *
     * @tags runs
     * @name SteerRun
     * @summary Steer a run
     * @request POST:/runs/{id}/steer
     */
    steerRun: (id: RunID, data: SteerRequest, params: RequestParams = {}) =>
      this.request<Run, Error>({
        path: `/runs/${id}/steer`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Keep the run working without a foreground session. The operator can still steer, review plans, and read events.
     *
     * @tags runs
     * @name BackgroundRun
     * @summary Background a run
     * @request POST:/runs/{id}/background
     */
    backgroundRun: (id: RunID, params: RequestParams = {}) =>
      this.request<Run, Error>({
        path: `/runs/${id}/background`,
        method: "POST",
        format: "json",
        ...params,
      }),
  };
  plans = {
    /**
     * No description
     *
     * @tags plans
     * @name GetPlan
     * @summary Get a plan
     * @request GET:/plans/{id}
     */
    getPlan: (id: PlanID, params: RequestParams = {}) =>
      this.request<Plan, Error>({
        path: `/plans/${id}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Human gate on the plan lifecycle. After approve, the daemon applies. There is no client apply operation; a UI or CLI must not invent one.
     *
     * @tags plans
     * @name ApprovePlan
     * @summary Approve a plan
     * @request POST:/plans/{id}/approve
     */
    approvePlan: (
      id: PlanID,
      data?: ApproveRequest,
      params: RequestParams = {},
    ) =>
      this.request<Plan, Error>({
        path: `/plans/${id}/approve`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags plans
     * @name RequestPlanChanges
     * @summary Request changes on a plan
     * @request POST:/plans/{id}/request-changes
     */
    requestPlanChanges: (
      id: PlanID,
      data: RequestChangesRequest,
      params: RequestParams = {},
    ) =>
      this.request<Plan, Error>({
        path: `/plans/${id}/request-changes`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Create an alternative plan from this one. The original plan is unchanged.
     *
     * @tags plans
     * @name BranchPlan
     * @summary Branch a plan
     * @request POST:/plans/{id}/branch
     */
    branchPlan: (
      id: PlanID,
      data?: BranchPlanRequest,
      params: RequestParams = {},
    ) =>
      this.request<Plan, Error>({
        path: `/plans/${id}/branch`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),
  };
  agents = {
    /**
     * No description
     *
     * @tags agents
     * @name ListAgents
     * @summary List agents
     * @request GET:/agents
     */
    listAgents: (
      query?: {
        /**
         * Opaque page cursor from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
        /**
         * Page size. The daemon defaults this when omitted.
         * @min 1
         * @max 200
         */
        limit?: number;
      },
      params: RequestParams = {},
    ) =>
      this.request<AgentList, any>({
        path: `/agents`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags agents
     * @name GetAgent
     * @summary Get an agent
     * @request GET:/agents/{id}
     */
    getAgent: (id: AgentID, params: RequestParams = {}) =>
      this.request<Agent, Error>({
        path: `/agents/${id}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Config changes are themselves signed audited actions. The daemon records and signs the patch; the client does not send a signature.
     *
     * @tags agents
     * @name PatchAgent
     * @summary Patch agent config
     * @request PATCH:/agents/{id}
     */
    patchAgent: (id: AgentID, data: AgentPatch, params: RequestParams = {}) =>
      this.request<Agent, Error>({
        path: `/agents/${id}`,
        method: "PATCH",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),
  };
  approvals = {
    /**
     * @description Operator inbox. Stage 1 items are plan approvals. Memory proposals are under /memory/proposals.
     *
     * @tags approvals
     * @name ListApprovals
     * @summary List approvals
     * @request GET:/approvals
     */
    listApprovals: (
      query?: {
        /**
         * Opaque page cursor from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
        /**
         * Page size. The daemon defaults this when omitted.
         * @min 1
         * @max 200
         */
        limit?: number;
        /** If set, only approvals in this status. */
        status?: ApprovalStatus;
      },
      params: RequestParams = {},
    ) =>
      this.request<ApprovalList, any>({
        path: `/approvals`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),
  };
  memory = {
    /**
     * No description
     *
     * @tags memory
     * @name ListMemory
     * @summary List memory entries
     * @request GET:/memory
     */
    listMemory: (
      query?: {
        /**
         * Opaque page cursor from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
        /**
         * Page size. The daemon defaults this when omitted.
         * @min 1
         * @max 200
         */
        limit?: number;
        kind?: MemoryKind;
        /**
         * Run id (session memory) or agent id (agent memory).
         * @minLength 1
         */
        ref_id?: string;
      },
      params: RequestParams = {},
    ) =>
      this.request<MemoryList, any>({
        path: `/memory`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description Operator-authored memory. Agent-proposed memory goes through /memory/proposals.
     *
     * @tags memory
     * @name CreateMemory
     * @summary Write a memory entry
     * @request POST:/memory
     */
    createMemory: (data: CreateMemoryRequest, params: RequestParams = {}) =>
      this.request<MemoryEntry, Error>({
        path: `/memory`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags memory
     * @name ListMemoryProposals
     * @summary List memory proposals
     * @request GET:/memory/proposals
     */
    listMemoryProposals: (
      query?: {
        /**
         * Opaque page cursor from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
        /**
         * Page size. The daemon defaults this when omitted.
         * @min 1
         * @max 200
         */
        limit?: number;
        status?: MemoryProposalStatus;
      },
      params: RequestParams = {},
    ) =>
      this.request<MemoryProposalList, any>({
        path: `/memory/proposals`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags memory
     * @name AcceptMemoryProposal
     * @summary Accept a memory proposal
     * @request POST:/memory/proposals/{id}/accept
     */
    acceptMemoryProposal: (id: MemoryProposalID, params: RequestParams = {}) =>
      this.request<MemoryProposal, Error>({
        path: `/memory/proposals/${id}/accept`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags memory
     * @name RejectMemoryProposal
     * @summary Reject a memory proposal
     * @request POST:/memory/proposals/{id}/reject
     */
    rejectMemoryProposal: (id: MemoryProposalID, params: RequestParams = {}) =>
      this.request<MemoryProposal, Error>({
        path: `/memory/proposals/${id}/reject`,
        method: "POST",
        format: "json",
        ...params,
      }),
  };
  audit = {
    /**
     * No description
     *
     * @tags audit
     * @name ListAudit
     * @summary List audit records
     * @request GET:/audit
     */
    listAudit: (
      query?: {
        /**
         * Opaque page cursor from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
        /**
         * Page size. The daemon defaults this when omitted.
         * @min 1
         * @max 200
         */
        limit?: number;
        resource_type?: AuditResourceType;
        /** @minLength 1 */
        resource_id?: string;
      },
      params: RequestParams = {},
    ) =>
      this.request<AuditList, any>({
        path: `/audit`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description Re-check the record's secp256k1 Schnorr signature (ADR-Z-0007). Does not rewrite the log.
     *
     * @tags audit
     * @name VerifyAudit
     * @summary Verify an audit record
     * @request POST:/audit/{id}/verify
     */
    verifyAudit: (id: AuditID, params: RequestParams = {}) =>
      this.request<AuditVerification, Error>({
        path: `/audit/${id}/verify`,
        method: "POST",
        format: "json",
        ...params,
      }),
  };
  checkpoints = {
    /**
     * No description
     *
     * @tags checkpoints
     * @name ListCheckpoints
     * @summary List checkpoints
     * @request GET:/checkpoints
     */
    listCheckpoints: (
      query?: {
        /**
         * Opaque page cursor from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
        /**
         * Page size. The daemon defaults this when omitted.
         * @min 1
         * @max 200
         */
        limit?: number;
        /** Opaque run id. Not interchangeable with other id kinds. */
        run_id?: RunID;
      },
      params: RequestParams = {},
    ) =>
      this.request<CheckpointList, any>({
        path: `/checkpoints`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description Restore run state from this checkpoint. Consequential; the daemon records a signed audit event and follows plan-then-apply if the restore mutates the world.
     *
     * @tags checkpoints
     * @name RestoreCheckpoint
     * @summary Restore a checkpoint
     * @request POST:/checkpoints/{id}/restore
     */
    restoreCheckpoint: (id: CheckpointID, params: RequestParams = {}) =>
      this.request<Run, Error>({
        path: `/checkpoints/${id}/restore`,
        method: "POST",
        format: "json",
        ...params,
      }),
  };
}
