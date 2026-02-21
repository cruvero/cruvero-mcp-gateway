# Phase 3A Implementation Prompts

## Prompt 1 of 3: Registration Types & Service

### Required Reading (read these files before writing code)
- docs/phases/PHASE3A.md
- internal/types/types.go
- internal/store/interfaces.go
- internal/identity/context.go
- internal/config/config.go

### Task

Create registration types and the service layer.

1. `internal/registration/types.go`:
   - Define RegistrationRequest struct with JSON tags: ServiceName, Version, Listen (ListenConfig), Capabilities (types.Capability), Labels (map[string]string)
   - Define ListenConfig struct: Host, Port (int), Protocol (string)
   - Define RegistrationResponse struct: InstanceID, PolicySnapshot (*types.PolicyProfile), HeartbeatIntervalSeconds (int), Status (types.ServerStatus)
   - Add Validate() error method on RegistrationRequest (check ServiceName non-empty, Port > 0)

2. `internal/registration/service.go`:
   - Define Service struct with serverStore, auditStore, config, logger fields
   - NewService constructor
   - Register method: validate request, build ServerRecord, store via Create, audit log, return response
   - List method: delegate to store with filter
   - Deregister method: check authorization (admin or self via SPIFFE ID match), delete from store, audit log

3. `internal/registration/service_test.go`:
   - Mock ServerStore and AuditStore
   - Test Register happy path
   - Test Register with invalid request (validation failure)
   - Test Deregister by admin
   - Test Deregister by self
   - Test Deregister unauthorized

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Types are well-defined with proper JSON tags
- Service encapsulates all business logic
- Authorization checks are correct
- All paths tested

---

## Prompt 2 of 3: Registration HTTP Handlers

### Required Reading (read these files before writing code)
- docs/phases/PHASE3A.md
- internal/registration/types.go
- internal/registration/service.go
- internal/identity/context.go
- internal/server/server.go

### Task

Create HTTP handlers for the registration API.

1. `internal/registration/handler.go`:
   - Define Handler struct wrapping Service and logger
   - NewHandler constructor
   - Routes() method returning chi.Router with:
     - POST / -> handleRegister
     - GET / -> handleList (requires admin scope)
     - DELETE /{id} -> handleDeregister
   - handleRegister: decode JSON body, extract Identity from context, call service.Register, return 201 with response
   - handleList: extract query params (status, name), call service.List, return 200 with JSON array
   - handleDeregister: extract {id} from URL, extract Identity, call service.Deregister, return 204
   - Error handling: map service errors to HTTP status codes (400, 401, 403, 404, 500)
   - JSON error response format: {"error": "message"}

2. `internal/registration/handler_test.go`:
   - Use httptest + chi router
   - Test POST with valid registration body -> 201
   - Test POST with invalid body -> 400
   - Test GET list -> 200 with array
   - Test DELETE -> 204
   - Test DELETE not found -> 404
   - Inject Identity into test request contexts

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- RESTful endpoint design
- Proper HTTP status codes
- JSON request/response handling
- Identity extracted from context correctly

---

## Prompt 3 of 3: Router Integration & End-to-End Tests

### Required Reading (read these files before writing code)
- internal/registration/handler.go
- internal/server/server.go
- internal/identity/middleware.go
- internal/auth/middleware.go

### Task

Wire registration routes into the server and add integration-style tests.

1. Update `internal/server/server.go` to mount registration routes:
   - Mount registration handler at /v1/registrations
   - Apply mTLS middleware to registration routes
   - Apply auth middleware + RBAC for admin-only routes

2. Write integration-style tests (still unit tests, no real database):
   - Test full flow: create mock stores -> create service -> create handler -> mount in router -> send requests
   - Test registration with mTLS identity in context -> 201
   - Test list requires admin scope -> 403 without, 200 with
   - Test deregister with matching SPIFFE ID -> 204
   - Test deregister with non-matching identity -> 403

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Routes mounted correctly in server
- Middleware chain applied in correct order
- Full request lifecycle tested
- >=80% coverage across registration package
