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

export enum MemoryProposalStatus {
  Pending = "pending",
  Accepted = "accepted",
  Rejected = "rejected",
}

/**
 * Mirrors the plan lifecycle names (draft, cross-exam, approve, apply)
 * plus the request-changes path. Applied is listed because apply exists
 * in the kernel even though this API has no apply operation.
 */
export enum PlanStatus {
  Draft = "draft",
  CrossExam = "cross_exam",
  PendingApproval = "pending_approval",
  Approved = "approved",
  ChangesRequested = "changes_requested",
  Applied = "applied",
}

/**
 * Draft enum so list and detail views can render a status. Closed
 * values are not locked against the session state machine.
 */
export enum RunStatus {
  Queued = "queued",
  Running = "running",
  Waiting = "waiting",
  Backgrounded = "backgrounded",
  Completed = "completed",
  Failed = "failed",
}

export interface Health {
  status: "ok";
}

export interface Error {
  /** Stable machine-readable code */
  error: string;
  message?: string;
}

/**
 * Opaque run identifier. Not interchangeable with other ID kinds.
 * @minLength 1
 */
export type RunID = string;

/**
 * Opaque plan identifier. Not interchangeable with other ID kinds.
 * @minLength 1
 */
export type PlanID = string;

/**
 * Opaque agent identifier. Not interchangeable with other ID kinds.
 * @minLength 1
 */
export type AgentID = string;

/**
 * Opaque memory entry identifier. Not interchangeable with other ID kinds.
 * @minLength 1
 */
export type MemoryID = string;

/**
 * Opaque memory proposal identifier. Not interchangeable with other ID kinds.
 * @minLength 1
 */
export type MemoryProposalID = string;

/**
 * Opaque audit record identifier. Not interchangeable with other ID kinds.
 * @minLength 1
 */
export type AuditID = string;

/**
 * Opaque checkpoint identifier. Not interchangeable with other ID kinds.
 * @minLength 1
 */
export type CheckpointID = string;

/**
 * Opaque approval identifier. Not interchangeable with other ID kinds.
 * @minLength 1
 */
export type ApprovalID = string;

/**
 * RFC 3339 timestamp
 * @format date-time
 */
export type Timestamp = string;

export interface Run {
  /** Opaque run identifier. Not interchangeable with other ID kinds. */
  id: RunID;
  /** Opaque agent identifier. Not interchangeable with other ID kinds. */
  agent_id: AgentID;
  /**
   * Draft enum so list and detail views can render a status. Closed
   * values are not locked against the session state machine.
   */
  status: RunStatus;
  /** Opaque plan identifier. Not interchangeable with other ID kinds. */
  current_plan_id?: PlanID;
  /** RFC 3339 timestamp */
  created_at: Timestamp;
  /** RFC 3339 timestamp */
  updated_at?: Timestamp;
}

export interface RunList {
  items: Run[];
  next_cursor?: string;
}

export interface CreateRunRequest {
  /** Opaque agent identifier. Not interchangeable with other ID kinds. */
  agent_id: AgentID;
  /**
   * Operator prompt that starts the run. Whether a tracker issue is
   * also required (and under which field) is not locked.
   * @minLength 1
   */
  prompt?: string;
}

export interface SteerRunRequest {
  /** @minLength 1 */
  message: string;
}

export interface RunEvent {
  /**
   * Monotonic per-run sequence used for replay order
   * @min 0
   */
  seq: number;
  /**
   * Event kind. A closed enum is not locked; the live view should not
   * assume these strings beyond opaque display until the session
   * machine lands.
   * @minLength 1
   */
  type: string;
  /** RFC 3339 timestamp */
  recorded_at: Timestamp;
  summary?: string;
}

export interface Plan {
  /** Opaque plan identifier. Not interchangeable with other ID kinds. */
  id: PlanID;
  /** Opaque run identifier. Not interchangeable with other ID kinds. */
  run_id: RunID;
  /**
   * Mirrors the plan lifecycle names (draft, cross-exam, approve, apply)
   * plus the request-changes path. Applied is listed because apply exists
   * in the kernel even though this API has no apply operation.
   */
  status: PlanStatus;
  title?: string;
  /**
   * Human-readable plan text. Structured diffs, file lists, and
   * command lists are not in this draft.
   */
  body?: string;
  /** RFC 3339 timestamp */
  created_at: Timestamp;
  /** RFC 3339 timestamp */
  updated_at?: Timestamp;
}

export interface ApprovePlanRequest {
  comment?: string;
}

export interface RequestPlanChangesRequest {
  /** @minLength 1 */
  comment: string;
}

export interface BranchPlanRequest {
  comment?: string;
}

export interface Agent {
  /** Opaque agent identifier. Not interchangeable with other ID kinds. */
  id: AgentID;
  /** @minLength 1 */
  name: string;
  /** Stage 1 has one harness implementation. */
  harness: "claudecode";
  /** RFC 3339 timestamp */
  created_at?: Timestamp;
  /** RFC 3339 timestamp */
  updated_at?: Timestamp;
}

export interface AgentList {
  items: Agent[];
  next_cursor?: string;
}

/**
 * Config fields beyond display name are not locked. Do not put
 * credentials here. Scopes and leases are policy, not agent config.
 */
export interface PatchAgentRequest {
  /** @minLength 1 */
  name?: string;
}

export interface Approval {
  /** Opaque approval identifier. Not interchangeable with other ID kinds. */
  id: ApprovalID;
  /**
   * Subject kind as an opaque string (for example plan or
   * memory_proposal). A closed enum is not locked.
   * @minLength 1
   */
  kind: string;
  /**
   * Identifier of the subject resource. Kind-specific.
   * @minLength 1
   */
  subject_id: string;
  /** Opaque run identifier. Not interchangeable with other ID kinds. */
  run_id?: RunID;
  summary?: string;
  /** RFC 3339 timestamp */
  created_at: Timestamp;
}

export interface ApprovalList {
  items: Approval[];
  next_cursor?: string;
}

export interface MemoryEntry {
  /** Opaque memory entry identifier. Not interchangeable with other ID kinds. */
  id: MemoryID;
  /** Opaque agent identifier. Not interchangeable with other ID kinds. */
  agent_id?: AgentID;
  /** Opaque run identifier. Not interchangeable with other ID kinds. */
  run_id?: RunID;
  /** @minLength 1 */
  body: string;
  /** RFC 3339 timestamp */
  created_at: Timestamp;
}

export interface MemoryEntryList {
  items: MemoryEntry[];
  next_cursor?: string;
}

export interface CreateMemoryRequest {
  /** @minLength 1 */
  body: string;
  /** Opaque agent identifier. Not interchangeable with other ID kinds. */
  agent_id?: AgentID;
  /** Opaque run identifier. Not interchangeable with other ID kinds. */
  run_id?: RunID;
}

export interface MemoryProposal {
  /** Opaque memory proposal identifier. Not interchangeable with other ID kinds. */
  id: MemoryProposalID;
  /** Opaque agent identifier. Not interchangeable with other ID kinds. */
  agent_id?: AgentID;
  /** Opaque run identifier. Not interchangeable with other ID kinds. */
  run_id?: RunID;
  /** @minLength 1 */
  body: string;
  status: MemoryProposalStatus;
  /** RFC 3339 timestamp */
  created_at: Timestamp;
}

export interface MemoryProposalList {
  items: MemoryProposal[];
  next_cursor?: string;
}

export interface AuditRecord {
  /** Opaque audit record identifier. Not interchangeable with other ID kinds. */
  id: AuditID;
  /** @minLength 1 */
  action: string;
  /** Opaque run identifier. Not interchangeable with other ID kinds. */
  run_id?: RunID;
  /** RFC 3339 timestamp */
  recorded_at: Timestamp;
  /** x-only secp256k1 public key, hex (ADR-Z-0007). Not a secret. */
  pubkey?: string;
  /** BIP-340 Schnorr signature, hex. Not a credential. */
  signature?: string;
}

export interface AuditRecordList {
  items: AuditRecord[];
  next_cursor?: string;
}

export interface AuditVerifyResult {
  valid: boolean;
  reason?: string;
}

export interface Checkpoint {
  /** Opaque checkpoint identifier. Not interchangeable with other ID kinds. */
  id: CheckpointID;
  /** Opaque run identifier. Not interchangeable with other ID kinds. */
  run_id: RunID;
  summary?: string;
  /** RFC 3339 timestamp */
  created_at: Timestamp;
}

export interface CheckpointList {
  items: Checkpoint[];
  next_cursor?: string;
}

export interface CheckpointRestore {
  /** Opaque checkpoint identifier. Not interchangeable with other ID kinds. */
  checkpoint_id: CheckpointID;
  /**
   * Run that results from restore, if restore mints or selects one.
   * Omitted until restore semantics are locked.
   */
  run_id?: RunID;
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
  public baseUrl: string = "http://127.0.0.1";
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
 * @baseUrl http://127.0.0.1
 * @contact (https://github.com/avivl/zeroth)
 *
 * Local control-plane API for Zeroth. Stage 1 is single-player and binds
 * locally. This spec is the canonical contract; Go stubs and the TypeScript
 * client are generated from it, not hand-written.
 *
 * Product version is the git commit SHA (plan §6), not this info.version
 * field. info.version stays a document label so the OpenAPI document remains
 * valid.
 *
 * HTTP "run" is the operator-facing name for a kernel session
 * (internal/session). Distinct kernel ID types stay distinct in Go; over
 * HTTP they are opaque strings and must not be mixed across resources.
 *
 * This document is a draft for Linear 42-17 (human-owned). Field-level
 * shapes that the mockup does not pin down are omitted rather than invented.
 * See pkg/api/README.md for the view mapping and the gaps that need a human.
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
         * @min 1
         * @max 100
         * @default 50
         */
        limit?: number;
        /**
         * Opaque page token from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
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
     * @description Upgrade this GET to a WebSocket. The server replays the last `last` recorded events, then tails live events until the socket closes. Each text frame is a JSON RunEvent. Generated HTTP clients will emit a GET helper; the web UI still needs a WebSocket caller for this path. That mismatch is called out in pkg/api/README.md rather than papered over with a second REST snapshot endpoint.
     *
     * @tags runs
     * @name GetRunEvents
     * @summary Run event stream
     * @request GET:/runs/{id}/events
     */
    getRunEvents: (
      id: RunID,
      query?: {
        /**
         * How many recorded events to replay before the live tail.
         * @min 0
         * @default 100
         */
        last?: number;
      },
      params: RequestParams = {},
    ) =>
      this.request<any, RunEvent | Error>({
        path: `/runs/${id}/events`,
        method: "GET",
        query: query,
        ...params,
      }),

    /**
     * @description Operator input into a live run (steer). This is not apply, and it does not bypass the plan lifecycle for consequential mutation.
     *
     * @tags runs
     * @name SteerRun
     * @summary Steer a run
     * @request POST:/runs/{id}/steer
     */
    steerRun: (id: RunID, data: SteerRunRequest, params: RequestParams = {}) =>
      this.request<Run, Error>({
        path: `/runs/${id}/steer`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Continue the run without a live foreground operator. There is no matching foreground endpoint on the listed stage-1 surface.
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
     * @description Human gate on a plan. Approve is not apply. There is no POST /plans/{id}/apply on the listed stage-1 surface; that gap is called out in pkg/api/README.md.
     *
     * @tags plans
     * @name ApprovePlan
     * @summary Approve a plan
     * @request POST:/plans/{id}/approve
     */
    approvePlan: (
      id: PlanID,
      data?: ApprovePlanRequest,
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
      data: RequestPlanChangesRequest,
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
     * @description Fork a new plan from this one. The original plan is not applied. Exact branch semantics (what is copied, whether the run switches) are not locked.
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
         * @min 1
         * @max 100
         * @default 50
         */
        limit?: number;
        /**
         * Opaque page token from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
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
     * @description Config changes are themselves signed, audited actions. The daemon records the audit event; the HTTP request is not a client-side signature. Credentials never appear in this body (ADR-Z-0008).
     *
     * @tags agents
     * @name PatchAgent
     * @summary Patch agent config
     * @request PATCH:/agents/{id}
     */
    patchAgent: (
      id: AgentID,
      data: PatchAgentRequest,
      params: RequestParams = {},
    ) =>
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
     * @description Human inbox of work that is waiting. This is not a decision endpoint; decide through the subject resource (plan, memory proposal, and so on).
     *
     * @tags approvals
     * @name ListApprovals
     * @summary List pending approvals
     * @request GET:/approvals
     */
    listApprovals: (
      query?: {
        /**
         * @min 1
         * @max 100
         * @default 50
         */
        limit?: number;
        /**
         * Opaque page token from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
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
         * @min 1
         * @max 100
         * @default 50
         */
        limit?: number;
        /**
         * Opaque page token from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
      },
      params: RequestParams = {},
    ) =>
      this.request<MemoryEntryList, any>({
        path: `/memory`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description Operator-authored memory. Agent-proposed writes go through /memory/proposals, not this path.
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
         * @min 1
         * @max 100
         * @default 50
         */
        limit?: number;
        /**
         * Opaque page token from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
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
         * @min 1
         * @max 100
         * @default 50
         */
        limit?: number;
        /**
         * Opaque page token from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
      },
      params: RequestParams = {},
    ) =>
      this.request<AuditRecordList, any>({
        path: `/audit`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description Re-verify the Schnorr signature (ADR-Z-0007) on one append-only record. This does not rewrite the log.
     *
     * @tags audit
     * @name VerifyAudit
     * @summary Verify an audit record
     * @request POST:/audit/{id}/verify
     */
    verifyAudit: (id: AuditID, params: RequestParams = {}) =>
      this.request<AuditVerifyResult, Error>({
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
         * @min 1
         * @max 100
         * @default 50
         */
        limit?: number;
        /**
         * Opaque page token from a previous next_cursor.
         * @minLength 1
         */
        cursor?: string;
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
     * @description Restore is a consequential action. It must still go through the plan lifecycle inside the daemon; this path is the operator trigger, not a kernel bypass. Whether restore mints a new run or rewinds in place is not locked.
     *
     * @tags checkpoints
     * @name RestoreCheckpoint
     * @summary Restore a checkpoint
     * @request POST:/checkpoints/{id}/restore
     */
    restoreCheckpoint: (id: CheckpointID, params: RequestParams = {}) =>
      this.request<CheckpointRestore, Error>({
        path: `/checkpoints/${id}/restore`,
        method: "POST",
        format: "json",
        ...params,
      }),
  };
}
