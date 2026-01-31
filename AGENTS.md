# AGENTS.md - Coding Agent Guidelines for cscan

## 交互要求
- 思考过程使用中文
- 回答也要用中文回复

## Project Overview

**cscan** - 企业级分布式网络资产扫描平台
- **Backend**: Go 1.25+ with go-zero microservice framework
- **Frontend**: Vue 3.4 + Vite + Element Plus + SCSS
- **Database**: MongoDB 6 + Redis 7
- **Scanning**: ProjectDiscovery tools (nuclei, httpx, subfinder, naabu, dnsx)

## Project Structure

```
cscan/
├── api/              # HTTP API (go-zero) - handlers, logic, svc, types
├── rpc/              # gRPC internal services
├── model/            # MongoDB data models
├── pkg/              # Shared packages (xerr, utils, response)
├── scanner/          # Scanning modules (nuclei, naabu, httpx, etc.)
├── scheduler/        # Task scheduling and load balancing
├── worker/           # Distributed worker processes
├── web/              # Vue.js frontend (src/views, src/components, src/stores)
└── docker/           # Docker configuration
```

## Build, Lint, and Test Commands

### Go Backend

```bash
# Build
go build -o cscan ./api/cscan.go
go build -o worker ./worker/

# Test - all
go test ./...

# Test - single file (use package path, not file path)
go test -v ./api/internal/svc/ -run TestFunctionName

# Test - specific function
go test -v -run TestProperty1_AssetResultAssociationCorrectness ./api/internal/svc/

# Test with coverage/race
go test -cover ./...
go test -race ./...

# Dependencies
go mod download && go mod tidy
```

### Vue Frontend (in `web/` directory)

```bash
npm install              # Install dependencies
npm run dev              # Dev server (port 3000)
npm run build            # Production build

# Testing
npm run test             # Run all tests (vitest)
npx vitest run src/tests/MyComponent.test.js  # Single test file
npm run test:coverage    # With coverage
```

## Code Style Guidelines

### Go Backend

#### Import Organization (3 groups, separated by blank lines)
```go
import (
    // 1. Standard library
    "context"
    "time"

    // 2. Internal packages
    "cscan/model"
    "cscan/pkg/xerr"

    // 3. Third-party packages
    "go.mongodb.org/mongo-driver/bson"
    "github.com/zeromicro/go-zero/core/logx"
)
```

#### Naming Conventions
| Element | Convention | Example |
|---------|------------|---------|
| Files | lowercase_underscore | `scanresult_service.go` |
| Packages | lowercase, single word | `model`, `svc`, `handler` |
| Structs/Types | PascalCase | `AssetModel`, `ScanResultService` |
| Interfaces | PascalCase, often `-er` suffix | `Scanner`, `AssetRepository` |
| Exported | PascalCase | `GetAsset()` |
| Unexported | camelCase | `parseResult()` |

#### Struct Tags (always include both)
```go
type Asset struct {
    Id        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
    Host      string             `bson:"host" json:"host"`
    RiskScore float64            `bson:"risk_score,omitempty" json:"riskScore,omitempty"`
}
```

#### Error Handling
```go
// Use pkg/xerr for business errors, wrap with context
if err != nil {
    return nil, xerr.NewErrCode(xerr.DbError)
}
```

#### Service Pattern (constructor with DI)
```go
type ScanResultService struct {
    db *mongo.Database
}

func NewScanResultService(db *mongo.Database) *ScanResultService {
    return &ScanResultService{db: db}
}
```

### Vue Frontend

- **Composition API**: Always use `<script setup>` syntax
- **UI Framework**: Element Plus components
- **Styling**: SCSS, use existing CSS variables for theming
- **Imports**: Use `@/` alias for `src/` directory
- **i18n**: Use `$t('key')` for all user-facing text
- **State**: Pinia stores in `src/stores/`

## Testing Guidelines

### Go - Property-Based Testing (gopter)
```go
func TestProperty_Example(t *testing.T) {
    parameters := gopter.DefaultTestParameters()
    parameters.MinSuccessfulTests = 100
    properties := gopter.NewProperties(parameters)
    properties.Property("description", prop.ForAll(
        func(input string) bool { return len(input) >= 0 },
        gen.AlphaString(),
    ))
    properties.TestingRun(t)
}
```

### Go - Table-Driven Tests
```go
func TestEdgeCase(t *testing.T) {
    tests := []struct{ name, input string; want int }{
        {"empty", "", 0},
        {"valid", "test", 4},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) { /* ... */ })
    }
}
```

### Vue - Vitest + happy-dom
```javascript
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'

describe('Component', () => {
    it('renders correctly', () => {
        const wrapper = mount(Component)
        expect(wrapper.text()).toContain('expected')
    })
})
```

## API & Database Conventions

### API
- Endpoints: `/api/v1/*` prefix
- Method: POST for most operations
- Auth: JWT in `Authorization` header

### MongoDB
- Collection naming: `{workspaceId}_{entity}` (e.g., `ws123_asset`)
- Always use `primitive.ObjectID` for `_id`
- Include `create_time` and `update_time` fields
- Create indexes for frequently queried fields

## Critical Rules for Agents

1. **Workspace Isolation**: ALWAYS filter by `workspace_id` for multi-tenant data
2. **Preserve User Data**: When updating assets, preserve `labels`, `memo`, `color_tag`
3. **API Stability**: Do NOT change existing endpoint paths or HTTP methods
4. **Error Codes**: Use codes from `pkg/xerr/errcode.go`
5. **Logging**: Use `logx` from go-zero for structured logging
6. **Legacy Data**: Handle missing newer fields gracefully (fallback logic)
7. **Type Safety**: Never use `as any`, `@ts-ignore`, or suppress type errors
