# ctx: Testing Specification

This document defines the testing strategy for ctx. Tests should be written alongside implementation — do not defer testing to the end.

## Testing Principles

1. **Test as you build**: Each phase in the implementation should include its tests before moving to the next phase
2. **Test at multiple levels**: Unit tests for functions, integration tests for commands, E2E tests for workflows
3. **Golden files for complex output**: Use golden file testing for parsers and formatters
4. **Table-driven tests**: Use Go's table-driven test pattern for comprehensive coverage
5. **Fail gracefully**: All error paths should be tested — the tool should never panic

## Directory Structure

```
ctx/
├── internal/
│   ├── db/
│   │   ├── db.go
│   │   ├── db_test.go
│   │   ├── nodes.go
│   │   ├── nodes_test.go
│   │   └── ...
│   ├── query/
│   │   ├── parser.go
│   │   ├── parser_test.go
│   │   ├── parser_fuzz_test.go
│   │   └── ...
│   └── hook/
│       ├── parser.go
│       ├── parser_test.go
│       └── ...
├── cmd/
│   └── ... (CLI commands)
├── e2e/
│   ├── basic_test.go
│   ├── workflow_test.go
│   └── claude_simulation_test.go
├── testdata/
│   ├── fixtures/
│   │   ├── nodes.json
│   │   └── edges.json
│   ├── queries/
│   │   ├── simple.txt
│   │   ├── simple.golden.json
│   │   └── ...
│   ├── responses/
│   │   ├── single_remember.txt
│   │   ├── single_remember.golden.json
│   │   ├── multi_command.txt
│   │   ├── multi_command.golden.json
│   │   └── ...
│   └── scenarios/
│       ├── basic_memory_cycle/
│       ├── summarization_provenance/
│       └── task_lifecycle/
├── testutil/
│   └── testutil.go
└── Makefile
```

---

## Layer 1: Database Operations (Unit Tests)

**File**: `internal/db/db_test.go`, `internal/db/nodes_test.go`, etc.

### What to Test

- Node CRUD (create, read, update, delete)
- Edge CRUD with foreign key constraints
- Tag operations (add, remove, list)
- FTS indexing and search
- Transaction rollback on failure
- Schema migrations

### Test Cases

```go
// internal/db/nodes_test.go

func TestNodeCreate(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    node, err := db.CreateNode(CreateNodeInput{
        Type:    "fact",
        Content: "test content",
    })
    
    assert.NoError(t, err)
    assert.NotEmpty(t, node.ID)
    assert.Equal(t, "fact", node.Type)
    assert.Equal(t, "test content", node.Content)
    assert.Greater(t, node.TokenEstimate, 0)
    assert.False(t, node.CreatedAt.IsZero())
}

func TestNodeCreate_AllTypes(t *testing.T) {
    validTypes := []string{"fact", "decision", "pattern", "observation", 
                          "hypothesis", "task", "summary", "source", "open-question"}
    
    for _, nodeType := range validTypes {
        t.Run(nodeType, func(t *testing.T) {
            db := testutil.SetupTestDB(t)
            
            node, err := db.CreateNode(CreateNodeInput{
                Type:    nodeType,
                Content: "test",
            })
            
            assert.NoError(t, err)
            assert.Equal(t, nodeType, node.Type)
        })
    }
}

func TestNodeCreate_InvalidType(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    _, err := db.CreateNode(CreateNodeInput{
        Type:    "invalid-type",
        Content: "test",
    })
    
    assert.Error(t, err)
}

func TestNodeGet(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    created, _ := db.CreateNode(CreateNodeInput{
        Type:    "fact",
        Content: "test content",
    })
    
    fetched, err := db.GetNode(created.ID)
    
    assert.NoError(t, err)
    assert.Equal(t, created.ID, fetched.ID)
    assert.Equal(t, created.Content, fetched.Content)
}

func TestNodeGet_NotFound(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    _, err := db.GetNode("nonexistent-id")
    
    assert.Error(t, err)
    assert.True(t, errors.Is(err, ErrNotFound))
}

func TestNodeUpdate(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    node, _ := db.CreateNode(CreateNodeInput{
        Type:    "fact",
        Content: "original",
    })
    
    updated, err := db.UpdateNode(node.ID, UpdateNodeInput{
        Content: ptr("updated content"),
    })
    
    assert.NoError(t, err)
    assert.Equal(t, "updated content", updated.Content)
    assert.True(t, updated.UpdatedAt.After(node.UpdatedAt))
}

func TestNodeDelete(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    node, _ := db.CreateNode(CreateNodeInput{
        Type:    "fact",
        Content: "to delete",
    })
    
    err := db.DeleteNode(node.ID)
    assert.NoError(t, err)
    
    _, err = db.GetNode(node.ID)
    assert.True(t, errors.Is(err, ErrNotFound))
}

func TestNodeList(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    // Create multiple nodes
    for i := 0; i < 5; i++ {
        db.CreateNode(CreateNodeInput{
            Type:    "fact",
            Content: fmt.Sprintf("node %d", i),
        })
    }
    
    nodes, err := db.ListNodes(ListOptions{})
    
    assert.NoError(t, err)
    assert.Len(t, nodes, 5)
}

func TestNodeList_FilterByType(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    db.CreateNode(CreateNodeInput{Type: "fact", Content: "b"})
    db.CreateNode(CreateNodeInput{Type: "decision", Content: "c"})
    
    nodes, err := db.ListNodes(ListOptions{Type: "fact"})
    
    assert.NoError(t, err)
    assert.Len(t, nodes, 2)
}

func TestNodeList_Limit(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    for i := 0; i < 10; i++ {
        db.CreateNode(CreateNodeInput{Type: "fact", Content: fmt.Sprintf("%d", i)})
    }
    
    nodes, err := db.ListNodes(ListOptions{Limit: 3})
    
    assert.NoError(t, err)
    assert.Len(t, nodes, 3)
}
```

### Edge Tests

```go
// internal/db/edges_test.go

func TestEdgeCreate(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    n1, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    n2, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "b"})
    
    edge, err := db.CreateEdge(n1.ID, n2.ID, "DEPENDS_ON")
    
    assert.NoError(t, err)
    assert.Equal(t, n1.ID, edge.FromID)
    assert.Equal(t, n2.ID, edge.ToID)
    assert.Equal(t, "DEPENDS_ON", edge.Type)
}

func TestEdgeCreate_InvalidFromNode(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    n1, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    
    _, err := db.CreateEdge("nonexistent", n1.ID, "DEPENDS_ON")
    
    assert.Error(t, err)
}

func TestEdgeCreate_Duplicate(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    n1, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    n2, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "b"})
    
    _, err1 := db.CreateEdge(n1.ID, n2.ID, "DEPENDS_ON")
    _, err2 := db.CreateEdge(n1.ID, n2.ID, "DEPENDS_ON")
    
    assert.NoError(t, err1)
    // Should either succeed silently (idempotent) or return specific error
    // Document which behavior is chosen
}

func TestEdgeCascadeDelete(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    n1, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    n2, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "b"})
    db.CreateEdge(n1.ID, n2.ID, "DEPENDS_ON")
    
    // Delete source node
    err := db.DeleteNode(n1.ID)
    assert.NoError(t, err)
    
    // Edge should be gone
    edges, _ := db.GetEdgesFrom(n1.ID)
    assert.Empty(t, edges)
}

func TestEdgeTypes(t *testing.T) {
    validTypes := []string{"DERIVED_FROM", "DEPENDS_ON", "SUPERSEDES", "RELATES_TO", "CHILD_OF"}
    
    for _, edgeType := range validTypes {
        t.Run(edgeType, func(t *testing.T) {
            db := testutil.SetupTestDB(t)
            
            n1, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
            n2, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "b"})
            
            edge, err := db.CreateEdge(n1.ID, n2.ID, edgeType)
            
            assert.NoError(t, err)
            assert.Equal(t, edgeType, edge.Type)
        })
    }
}
```

### Tag Tests

```go
// internal/db/tags_test.go

func TestTagAdd(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    node, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    
    err := db.AddTag(node.ID, "project:test")
    
    assert.NoError(t, err)
    
    tags, _ := db.GetTags(node.ID)
    assert.Contains(t, tags, "project:test")
}

func TestTagAdd_Idempotent(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    node, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    
    err1 := db.AddTag(node.ID, "project:test")
    err2 := db.AddTag(node.ID, "project:test")
    
    assert.NoError(t, err1)
    assert.NoError(t, err2) // Should succeed silently
    
    tags, _ := db.GetTags(node.ID)
    assert.Len(t, tags, 1) // Not duplicated
}

func TestTagRemove(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    node, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    db.AddTag(node.ID, "project:test")
    
    err := db.RemoveTag(node.ID, "project:test")
    
    assert.NoError(t, err)
    
    tags, _ := db.GetTags(node.ID)
    assert.NotContains(t, tags, "project:test")
}

func TestTagList_AllTags(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    n1, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    n2, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "b"})
    
    db.AddTag(n1.ID, "project:a")
    db.AddTag(n1.ID, "tier:reference")
    db.AddTag(n2.ID, "project:b")
    
    tags, err := db.ListAllTags()
    
    assert.NoError(t, err)
    assert.Len(t, tags, 3)
}

func TestTagList_ByPrefix(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    node, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    db.AddTag(node.ID, "project:a")
    db.AddTag(node.ID, "project:b")
    db.AddTag(node.ID, "tier:reference")
    
    tags, err := db.ListTagsByPrefix("project:")
    
    assert.NoError(t, err)
    assert.Len(t, tags, 2)
}
```

### FTS Tests

```go
// internal/db/fts_test.go

func TestFTSSearch(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    db.CreateNode(CreateNodeInput{Type: "fact", Content: "The quick brown fox"})
    db.CreateNode(CreateNodeInput{Type: "fact", Content: "The lazy dog"})
    db.CreateNode(CreateNodeInput{Type: "fact", Content: "Something else entirely"})
    
    results, err := db.Search("quick fox")
    
    assert.NoError(t, err)
    assert.Len(t, results, 1)
    assert.Contains(t, results[0].Content, "quick")
}

func TestFTSSearch_NoResults(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    db.CreateNode(CreateNodeInput{Type: "fact", Content: "The quick brown fox"})
    
    results, err := db.Search("elephant")
    
    assert.NoError(t, err)
    assert.Empty(t, results)
}

func TestFTSSearch_UpdatedContent(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    node, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "original content"})
    
    // Should find original
    results1, _ := db.Search("original")
    assert.Len(t, results1, 1)
    
    // Update content
    db.UpdateNode(node.ID, UpdateNodeInput{Content: ptr("updated content")})
    
    // Should not find original anymore
    results2, _ := db.Search("original")
    assert.Empty(t, results2)
    
    // Should find updated
    results3, _ := db.Search("updated")
    assert.Len(t, results3, 1)
}
```

---

## Layer 2: Query Language (Unit Tests)

**File**: `internal/query/parser_test.go`, `internal/query/executor_test.go`

### Parser Tests

```go
// internal/query/parser_test.go

func TestQueryParser(t *testing.T) {
    cases := []struct {
        name    string
        input   string
        wantAST *QueryAST
        wantErr bool
    }{
        {
            name:  "simple type predicate",
            input: "type:fact",
            wantAST: &QueryAST{
                Type:  "predicate",
                Key:   "type",
                Value: "fact",
            },
        },
        {
            name:  "simple tag predicate",
            input: "tag:project:ctx",
            wantAST: &QueryAST{
                Type:  "predicate",
                Key:   "tag",
                Value: "project:ctx",
            },
        },
        {
            name:  "AND expression",
            input: "type:fact AND tag:project:ctx",
            wantAST: &QueryAST{
                Type: "and",
                Left: &QueryAST{
                    Type: "predicate", Key: "type", Value: "fact",
                },
                Right: &QueryAST{
                    Type: "predicate", Key: "tag", Value: "project:ctx",
                },
            },
        },
        {
            name:  "OR expression",
            input: "type:fact OR type:decision",
            wantAST: &QueryAST{
                Type: "or",
                Left: &QueryAST{
                    Type: "predicate", Key: "type", Value: "fact",
                },
                Right: &QueryAST{
                    Type: "predicate", Key: "type", Value: "decision",
                },
            },
        },
        {
            name:  "NOT expression",
            input: "NOT type:fact",
            wantAST: &QueryAST{
                Type: "not",
                Child: &QueryAST{
                    Type: "predicate", Key: "type", Value: "fact",
                },
            },
        },
        {
            name:  "parentheses",
            input: "(type:fact OR type:decision) AND tag:project:x",
            // Test precedence is correct
        },
        {
            name:  "created time filter - relative",
            input: "created:>24h",
            wantAST: &QueryAST{
                Type:     "predicate",
                Key:      "created",
                Operator: ">",
                Value:    "24h",
            },
        },
        {
            name:  "created time filter - absolute",
            input: "created:>2024-01-01",
            wantAST: &QueryAST{
                Type:     "predicate",
                Key:      "created",
                Operator: ">",
                Value:    "2024-01-01",
            },
        },
        {
            name:  "tokens filter",
            input: "tokens:<1000",
            wantAST: &QueryAST{
                Type:     "predicate",
                Key:      "tokens",
                Operator: "<",
                Value:    "1000",
            },
        },
        {
            name:  "has predicate",
            input: "has:summary",
            wantAST: &QueryAST{
                Type:  "predicate",
                Key:   "has",
                Value: "summary",
            },
        },
        {
            name:    "empty query",
            input:   "",
            wantAST: nil, // or empty AST that matches all
        },
        {
            name:    "malformed - missing value",
            input:   "type:",
            wantErr: true,
        },
        {
            name:    "malformed - unclosed paren",
            input:   "(type:fact",
            wantErr: true,
        },
        {
            name:    "malformed - unknown key",
            input:   "unknown:value",
            wantErr: true,
        },
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            ast, err := Parse(tc.input)
            
            if tc.wantErr {
                assert.Error(t, err)
                return
            }
            
            assert.NoError(t, err)
            assert.Equal(t, tc.wantAST, ast)
        })
    }
}
```

### Fuzz Testing

```go
// internal/query/parser_fuzz_test.go

func FuzzQueryParser(f *testing.F) {
    // Seed corpus
    f.Add("type:fact")
    f.Add("tag:project:x AND tag:tier:reference")
    f.Add("(type:fact OR type:decision) AND NOT tag:archived")
    f.Add("")
    f.Add("type:fact AND type:fact AND type:fact")
    f.Add("((((type:fact))))")
    
    f.Fuzz(func(t *testing.T, input string) {
        // Should never panic, regardless of input
        _, _ = Parse(input)
    })
}
```

### Executor Tests

```go
// internal/query/executor_test.go

func TestQueryExecution(t *testing.T) {
    cases := []struct {
        name     string
        setup    func(db *DB)
        query    string
        wantIDs  []string // or check count
        wantErr  bool
    }{
        {
            name: "type filter",
            setup: func(db *DB) {
                db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
                db.CreateNode(CreateNodeInput{Type: "fact", Content: "b"})
                db.CreateNode(CreateNodeInput{Type: "decision", Content: "c"})
            },
            query:   "type:fact",
            wantIDs: nil, // Check count instead
        },
        {
            name: "tag filter",
            setup: func(db *DB) {
                n1, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
                n2, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "b"})
                db.AddTag(n1.ID, "project:x")
            },
            query: "tag:project:x",
        },
        {
            name: "combined AND",
            setup: func(db *DB) {
                n1, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
                n2, _ := db.CreateNode(CreateNodeInput{Type: "decision", Content: "b"})
                db.AddTag(n1.ID, "project:x")
                db.AddTag(n2.ID, "project:x")
            },
            query: "type:fact AND tag:project:x",
        },
        {
            name: "time filter - recent",
            setup: func(db *DB) {
                db.CreateNode(CreateNodeInput{Type: "fact", Content: "recent"})
                // Would need to manipulate time or use old fixture
            },
            query: "created:>1h",
        },
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            db := testutil.SetupTestDB(t)
            tc.setup(db)
            
            results, err := ExecuteQuery(db, tc.query)
            
            if tc.wantErr {
                assert.Error(t, err)
                return
            }
            
            assert.NoError(t, err)
            // Add specific assertions based on test case
        })
    }
}
```

---

## Layer 3: Hook Command Parsing (Unit Tests)

**File**: `internal/hook/parser_test.go`

This is critical — parsing Claude's `<ctx:*>` output must be robust.

### Parser Tests

```go
// internal/hook/parser_test.go

func TestParseCtxCommands(t *testing.T) {
    cases := []struct {
        name  string
        input string
        want  []CtxCommand
    }{
        {
            name: "simple remember",
            input: `Here's my analysis.

<ctx:remember type="fact" tags="project:x,tier:reference">
The API uses OAuth 2.0.
</ctx:remember>

Let me know if you need more.`,
            want: []CtxCommand{
                {
                    Type: "remember",
                    Attrs: map[string]string{
                        "type": "fact",
                        "tags": "project:x,tier:reference",
                    },
                    Content: "The API uses OAuth 2.0.",
                },
            },
        },
        {
            name: "remember with multiline content",
            input: `<ctx:remember type="decision" tags="tier:reference">
We decided to use PostgreSQL.

Rationale:
- Better concurrency
- Native JSON support
- Team familiarity
</ctx:remember>`,
            want: []CtxCommand{
                {
                    Type: "remember",
                    Attrs: map[string]string{
                        "type": "decision",
                        "tags": "tier:reference",
                    },
                    Content: "We decided to use PostgreSQL.\n\nRationale:\n- Better concurrency\n- Native JSON support\n- Team familiarity",
                },
            },
        },
        {
            name: "self-closing recall",
            input: `Let me check what we discussed.
<ctx:recall query="type:fact AND tag:auth"/>`,
            want: []CtxCommand{
                {
                    Type: "recall",
                    Attrs: map[string]string{
                        "query": "type:fact AND tag:auth",
                    },
                },
            },
        },
        {
            name: "self-closing with spaces",
            input: `<ctx:recall query="type:fact" />`,
            want: []CtxCommand{
                {
                    Type:  "recall",
                    Attrs: map[string]string{"query": "type:fact"},
                },
            },
        },
        {
            name: "status command",
            input: `Let me check my memory state.
<ctx:status/>`,
            want: []CtxCommand{
                {Type: "status"},
            },
        },
        {
            name: "multiple commands",
            input: `<ctx:remember type="fact">Fact one</ctx:remember>
Some text in between.
<ctx:remember type="decision">Decision one</ctx:remember>
<ctx:link from="abc" to="def" type="DEPENDS_ON"/>`,
            want: []CtxCommand{
                {Type: "remember", Attrs: map[string]string{"type": "fact"}, Content: "Fact one"},
                {Type: "remember", Attrs: map[string]string{"type": "decision"}, Content: "Decision one"},
                {Type: "link", Attrs: map[string]string{"from": "abc", "to": "def", "type": "DEPENDS_ON"}},
            },
        },
        {
            name: "summarize with archive",
            input: `<ctx:summarize nodes="01HQ1234,01HQ5678" archive="true">
The auth system uses OIDC with custom claims.
</ctx:summarize>`,
            want: []CtxCommand{
                {
                    Type: "summarize",
                    Attrs: map[string]string{
                        "nodes":   "01HQ1234,01HQ5678",
                        "archive": "true",
                    },
                    Content: "The auth system uses OIDC with custom claims.",
                },
            },
        },
        {
            name: "task start",
            input: `<ctx:task name="implement-auth" action="start"/>`,
            want: []CtxCommand{
                {
                    Type: "task",
                    Attrs: map[string]string{
                        "name":   "implement-auth",
                        "action": "start",
                    },
                },
            },
        },
        {
            name: "task end with summarize",
            input: `<ctx:task name="implement-auth" action="end" summarize="true"/>`,
            want: []CtxCommand{
                {
                    Type: "task",
                    Attrs: map[string]string{
                        "name":      "implement-auth",
                        "action":    "end",
                        "summarize": "true",
                    },
                },
            },
        },
        {
            name: "expand command",
            input: `<ctx:expand node="01HQ1234"/>`,
            want: []CtxCommand{
                {
                    Type:  "expand",
                    Attrs: map[string]string{"node": "01HQ1234"},
                },
            },
        },
        {
            name: "supersede command",
            input: `<ctx:supersede old="01HQ1234" new="01HQ5678"/>`,
            want: []CtxCommand{
                {
                    Type: "supersede",
                    Attrs: map[string]string{
                        "old": "01HQ1234",
                        "new": "01HQ5678",
                    },
                },
            },
        },
        {
            name:  "ignore commands in code blocks",
            input: "Here's an example:\n```xml\n<ctx:remember type=\"fact\">ignored</ctx:remember>\n```",
            want:  []CtxCommand{}, // Should be empty
        },
        {
            name:  "ignore commands in inline code",
            input: "Use `<ctx:remember type=\"fact\">` to store facts.",
            want:  []CtxCommand{}, // Should be empty
        },
        {
            name:  "no commands",
            input: "This is just regular text with no ctx commands.",
            want:  []CtxCommand{},
        },
        {
            name:  "malformed - unclosed tag",
            input: `<ctx:remember type="fact">content without closing`,
            want:  []CtxCommand{}, // Gracefully ignore
        },
        {
            name:  "malformed - missing required attr",
            input: `<ctx:remember>content</ctx:remember>`,
            want:  []CtxCommand{}, // Or return with validation error
        },
        {
            name: "quotes in content",
            input: `<ctx:remember type="fact">User said "hello world"</ctx:remember>`,
            want: []CtxCommand{
                {
                    Type:    "remember",
                    Attrs:   map[string]string{"type": "fact"},
                    Content: `User said "hello world"`,
                },
            },
        },
        {
            name: "special characters in content",
            input: `<ctx:remember type="fact">Code: if (x < y && z > 0)</ctx:remember>`,
            want: []CtxCommand{
                {
                    Type:    "remember",
                    Attrs:   map[string]string{"type": "fact"},
                    Content: "Code: if (x < y && z > 0)",
                },
            },
        },
        {
            name: "unicode content",
            input: `<ctx:remember type="fact">User speaks 日本語 and uses emoji 🚀</ctx:remember>`,
            want: []CtxCommand{
                {
                    Type:    "remember",
                    Attrs:   map[string]string{"type": "fact"},
                    Content: "User speaks 日本語 and uses emoji 🚀",
                },
            },
        },
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := ParseCtxCommands(tc.input)
            assert.Equal(t, tc.want, got)
        })
    }
}
```

### Golden File Tests

```go
// internal/hook/parser_golden_test.go

var updateGolden = flag.Bool("update", false, "update golden files")

func TestParseGoldenFiles(t *testing.T) {
    files, err := filepath.Glob("testdata/responses/*.txt")
    require.NoError(t, err)
    
    for _, f := range files {
        name := strings.TrimSuffix(filepath.Base(f), ".txt")
        t.Run(name, func(t *testing.T) {
            input, err := os.ReadFile(f)
            require.NoError(t, err)
            
            got := ParseCtxCommands(string(input))
            
            goldenPath := strings.TrimSuffix(f, ".txt") + ".golden.json"
            
            if *updateGolden {
                data, _ := json.MarshalIndent(got, "", "  ")
                os.WriteFile(goldenPath, data, 0644)
                return
            }
            
            wantData, err := os.ReadFile(goldenPath)
            require.NoError(t, err)
            
            var want []CtxCommand
            json.Unmarshal(wantData, &want)
            
            assert.Equal(t, want, got)
        })
    }
}
```

---

## Layer 4: View Composition (Unit Tests)

**File**: `internal/view/composer_test.go`

```go
// internal/view/composer_test.go

func TestComposeBudgetEnforcement(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    // Create nodes totaling ~10000 tokens (each ~100 tokens)
    for i := 0; i < 100; i++ {
        db.CreateNode(CreateNodeInput{
            Type:    "fact",
            Content: strings.Repeat("word ", 25), // ~100 tokens
        })
    }
    
    result, err := Compose(db, ComposeOptions{
        Query:  "type:fact",
        Budget: 5000,
    })
    
    assert.NoError(t, err)
    assert.LessOrEqual(t, result.TotalTokens, 5000)
    assert.Less(t, len(result.Nodes), 100)
}

func TestComposePrioritySorting(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    // Create nodes with different tiers
    pinned, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "pinned"})
    db.AddTag(pinned.ID, "tier:pinned")
    
    ref, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "reference"})
    db.AddTag(ref.ID, "tier:reference")
    
    working, _ := db.CreateNode(CreateNodeInput{Type: "fact", Content: "working"})
    db.AddTag(working.ID, "tier:working")
    
    result, _ := Compose(db, ComposeOptions{
        Query:  "type:fact",
        Budget: 200, // Only room for 2 nodes
    })
    
    // Pinned should always be first
    assert.Equal(t, pinned.ID, result.Nodes[0].ID)
    // Reference before working
    assert.Equal(t, ref.ID, result.Nodes[1].ID)
}

func TestComposeEmptyQuery(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    db.CreateNode(CreateNodeInput{Type: "decision", Content: "b"})
    
    result, err := Compose(db, ComposeOptions{
        Query:  "",
        Budget: 10000,
    })
    
    assert.NoError(t, err)
    assert.Len(t, result.Nodes, 2) // Should return all
}

func TestComposeZeroBudget(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    db.CreateNode(CreateNodeInput{Type: "fact", Content: "a"})
    
    result, err := Compose(db, ComposeOptions{
        Query:  "type:fact",
        Budget: 0,
    })
    
    // Define expected behavior: error or empty result?
    assert.NoError(t, err)
    assert.Empty(t, result.Nodes)
}

func TestComposeSingleNodeExceedsBudget(t *testing.T) {
    db := testutil.SetupTestDB(t)
    
    // Create a very large node
    db.CreateNode(CreateNodeInput{
        Type:    "fact",
        Content: strings.Repeat("word ", 10000), // ~40000 tokens
    })
    
    result, err := Compose(db, ComposeOptions{
        Query:  "type:fact",
        Budget: 1000,
    })
    
    // Define expected behavior: include it anyway? skip? truncate?
    // Document the decision
    assert.NoError(t, err)
}
```

---

## Layer 5: CLI Commands (Integration Tests)

**File**: `cmd/cmd_test.go` or per-command test files

```go
// cmd/add_test.go

func TestCLIAdd(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    output := testutil.RunCtx(t, "--db", dbPath, "add",
        "--type", "fact",
        "--tag", "project:test",
        "This is test content")
    
    assert.Contains(t, output, "Added:")
    
    // Verify via list
    listOutput := testutil.RunCtx(t, "--db", dbPath, "list", "--format", "json")
    
    var nodes []Node
    json.Unmarshal([]byte(listOutput), &nodes)
    
    assert.Len(t, nodes, 1)
    assert.Equal(t, "fact", nodes[0].Type)
    assert.Equal(t, "This is test content", nodes[0].Content)
}

func TestCLIAdd_Stdin(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    cmd := exec.Command("ctx", "--db", dbPath, "add", "--type", "fact", "--stdin")
    cmd.Stdin = strings.NewReader("Content from stdin")
    
    output, err := cmd.CombinedOutput()
    
    assert.NoError(t, err)
    assert.Contains(t, string(output), "Added:")
}

func TestCLIAdd_MissingType(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    cmd := exec.Command("ctx", "--db", dbPath, "add", "content without type")
    output, err := cmd.CombinedOutput()
    
    assert.Error(t, err)
    assert.Contains(t, string(output), "required flag")
}

func TestCLIQuery(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // Seed data
    testutil.RunCtx(t, "--db", dbPath, "add", "--type", "fact", "Fact A")
    testutil.RunCtx(t, "--db", dbPath, "add", "--type", "decision", "Decision B")
    
    output := testutil.RunCtx(t, "--db", dbPath, "query", "type:fact", "--format", "json")
    
    var nodes []Node
    json.Unmarshal([]byte(output), &nodes)
    
    assert.Len(t, nodes, 1)
    assert.Equal(t, "Fact A", nodes[0].Content)
}

func TestCLICompose(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // Seed data
    testutil.RunCtx(t, "--db", dbPath, "add", "--type", "fact", "--tag", "tier:reference", "Important fact")
    
    output := testutil.RunCtx(t, "--db", dbPath, "compose", "--budget", "10000", "--format", "markdown")
    
    assert.Contains(t, output, "<!-- ctx:")
    assert.Contains(t, output, "Important fact")
}

func TestCLISummarize(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // Create source nodes
    out1 := testutil.RunCtx(t, "--db", dbPath, "add", "--type", "observation", "Obs 1")
    id1 := extractID(out1)
    
    out2 := testutil.RunCtx(t, "--db", dbPath, "add", "--type", "observation", "Obs 2")
    id2 := extractID(out2)
    
    // Summarize
    testutil.RunCtx(t, "--db", dbPath, "summarize", id1, id2,
        "--content", "Summary of observations",
        "--archive-sources")
    
    // Verify summary exists
    listOutput := testutil.RunCtx(t, "--db", dbPath, "list", "--type", "summary", "--format", "json")
    
    var nodes []Node
    json.Unmarshal([]byte(listOutput), &nodes)
    
    assert.Len(t, nodes, 1)
    
    // Verify edges
    edgesOutput := testutil.RunCtx(t, "--db", dbPath, "edges", nodes[0].ID, "--format", "json")
    
    var edges []Edge
    json.Unmarshal([]byte(edgesOutput), &edges)
    
    assert.Len(t, edges, 2) // DERIVED_FROM to both sources
}
```

---

## Layer 6: Hook Integration (Integration Tests)

**File**: `cmd/hook/hook_test.go`

```go
// cmd/hook/hook_test.go

func TestHookSessionStart(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // Seed database
    testutil.RunCtx(t, "--db", dbPath, "add", "--type", "fact", "--tag", "tier:reference", "Test fact")
    
    // Simulate Claude Code's SessionStart input
    input := `{
        "session_id": "abc123",
        "cwd": "/tmp/project",
        "hook_event_name": "SessionStart",
        "source": "startup"
    }`
    
    cmd := exec.Command("ctx", "--db", dbPath, "hook", "session-start")
    cmd.Stdin = strings.NewReader(input)
    
    output, err := cmd.Output()
    assert.NoError(t, err)
    
    var result map[string]interface{}
    json.Unmarshal(output, &result)
    
    hookOutput := result["hookSpecificOutput"].(map[string]interface{})
    assert.Equal(t, "SessionStart", hookOutput["hookEventName"])
    
    additionalContext := hookOutput["additionalContext"].(string)
    assert.Contains(t, additionalContext, "Test fact")
}

func TestHookSessionStart_EmptyDB(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    input := `{"session_id": "abc123", "hook_event_name": "SessionStart"}`
    
    cmd := exec.Command("ctx", "--db", dbPath, "hook", "session-start")
    cmd.Stdin = strings.NewReader(input)
    
    output, err := cmd.Output()
    assert.NoError(t, err)
    
    var result map[string]interface{}
    json.Unmarshal(output, &result)
    
    // Should still return valid JSON, just with empty/minimal context
    hookOutput := result["hookSpecificOutput"].(map[string]interface{})
    assert.Contains(t, hookOutput["additionalContext"], "<!-- ctx: 0 nodes")
}

func TestHookStop_ProcessesRemember(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // Initialize empty db
    testutil.RunCtx(t, "--db", dbPath, "status")
    
    // Claude's response with ctx command
    response := `Here's what I found.

<ctx:remember type="fact" tags="tier:reference">
New knowledge learned.
</ctx:remember>

Let me know if you need anything else.`
    
    input := fmt.Sprintf(`{
        "session_id": "abc123",
        "hook_event_name": "Stop"
    }`)
    
    cmd := exec.Command("ctx", "--db", dbPath, "hook", "stop", "--response", response)
    cmd.Stdin = strings.NewReader(input)
    
    err := cmd.Run()
    assert.NoError(t, err)
    
    // Verify node was created
    listOutput := testutil.RunCtx(t, "--db", dbPath, "list", "--format", "json")
    
    var nodes []Node
    json.Unmarshal([]byte(listOutput), &nodes)
    
    assert.Len(t, nodes, 1)
    assert.Equal(t, "fact", nodes[0].Type)
    assert.Contains(t, nodes[0].Content, "New knowledge learned")
}

func TestHookStop_ProcessesRecall(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // Seed data
    testutil.RunCtx(t, "--db", dbPath, "add", "--type", "fact", "--tag", "topic:auth", "Auth uses OAuth")
    
    response := `<ctx:recall query="tag:topic:auth"/>`
    
    cmd := exec.Command("ctx", "--db", dbPath, "hook", "stop", "--response", response)
    cmd.Stdin = strings.NewReader(`{"hook_event_name": "Stop"}`)
    
    err := cmd.Run()
    assert.NoError(t, err)
    
    // Next prompt-submit should include results
    cmd2 := exec.Command("ctx", "--db", dbPath, "hook", "prompt-submit")
    cmd2.Stdin = strings.NewReader(`{"hook_event_name": "UserPromptSubmit", "prompt": "test"}`)
    
    output, _ := cmd2.Output()
    
    var result map[string]interface{}
    json.Unmarshal(output, &result)
    
    hookOutput := result["hookSpecificOutput"].(map[string]interface{})
    additionalContext := hookOutput["additionalContext"].(string)
    
    assert.Contains(t, additionalContext, "Auth uses OAuth")
}

func TestHookStop_MultipleCommands(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    response := `<ctx:remember type="fact">Fact one</ctx:remember>
<ctx:remember type="decision">Decision one</ctx:remember>`
    
    cmd := exec.Command("ctx", "--db", dbPath, "hook", "stop", "--response", response)
    cmd.Stdin = strings.NewReader(`{"hook_event_name": "Stop"}`)
    
    err := cmd.Run()
    assert.NoError(t, err)
    
    listOutput := testutil.RunCtx(t, "--db", dbPath, "list", "--format", "json")
    
    var nodes []Node
    json.Unmarshal([]byte(listOutput), &nodes)
    
    assert.Len(t, nodes, 2)
}

func TestHookStop_IgnoresCodeBlocks(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    response := "Here's an example:\n```xml\n<ctx:remember type=\"fact\">should be ignored</ctx:remember>\n```"
    
    cmd := exec.Command("ctx", "--db", dbPath, "hook", "stop", "--response", response)
    cmd.Stdin = strings.NewReader(`{"hook_event_name": "Stop"}`)
    
    err := cmd.Run()
    assert.NoError(t, err)
    
    listOutput := testutil.RunCtx(t, "--db", dbPath, "list", "--format", "json")
    
    var nodes []Node
    json.Unmarshal([]byte(listOutput), &nodes)
    
    assert.Empty(t, nodes) // Nothing should be added
}
```

---

## Layer 7: End-to-End Scenarios

**File**: `e2e/scenarios_test.go`

```go
// e2e/scenarios_test.go

func TestE2E_BasicMemoryCycle(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // 1. Session starts, empty context
    startOutput := runSessionStart(t, dbPath)
    assert.Contains(t, startOutput.AdditionalContext, "<!-- ctx: 0 nodes")
    
    // 2. Claude responds with remember command
    response1 := `I'll remember that.
<ctx:remember type="fact" tags="tier:reference">User prefers Go for backend services.</ctx:remember>`
    
    runStop(t, dbPath, response1)
    
    // 3. New session starts, should include the fact
    startOutput2 := runSessionStart(t, dbPath)
    assert.Contains(t, startOutput2.AdditionalContext, "User prefers Go")
    
    // 4. Claude recalls
    response2 := `<ctx:recall query="type:fact"/>`
    runStop(t, dbPath, response2)
    
    // 5. Next prompt should include recall results
    promptOutput := runPromptSubmit(t, dbPath, "tell me about Go")
    assert.Contains(t, promptOutput.AdditionalContext, "User prefers Go")
}

func TestE2E_SummarizationWithProvenance(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // 1. Add several observations via stop hook
    for i := 0; i < 3; i++ {
        response := fmt.Sprintf(`<ctx:remember type="observation" tags="tier:working">Observation %d about the bug.</ctx:remember>`, i)
        runStop(t, dbPath, response)
    }
    
    // 2. Get node IDs
    nodes := listNodes(t, dbPath)
    ids := make([]string, len(nodes))
    for i, n := range nodes {
        ids[i] = n.ID
    }
    
    // 3. Claude summarizes
    response := fmt.Sprintf(`<ctx:summarize nodes="%s" archive="true">
The bug was caused by a race condition in cache invalidation.
</ctx:summarize>`, strings.Join(ids, ","))
    
    runStop(t, dbPath, response)
    
    // 4. Verify summary exists with edges
    nodes = listNodes(t, dbPath)
    var summaryNode *Node
    for _, n := range nodes {
        if n.Type == "summary" {
            summaryNode = &n
            break
        }
    }
    assert.NotNil(t, summaryNode)
    
    // 5. Check edges
    edges := getEdges(t, dbPath, summaryNode.ID)
    assert.Len(t, edges, 3) // Three DERIVED_FROM edges
    
    // 6. Original nodes should be off-context
    for _, id := range ids {
        node := getNode(t, dbPath, id)
        assert.Contains(t, node.Tags, "tier:off-context")
    }
    
    // 7. Trace should show provenance
    traceOutput := testutil.RunCtx(t, "--db", dbPath, "trace", summaryNode.ID)
    for _, id := range ids {
        assert.Contains(t, traceOutput, id)
    }
    
    // 8. New session should include summary, not original observations
    startOutput := runSessionStart(t, dbPath)
    assert.Contains(t, startOutput.AdditionalContext, "race condition")
    assert.NotContains(t, startOutput.AdditionalContext, "Observation 0")
}

func TestE2E_TaskLifecycle(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // 1. Start a task
    runStop(t, dbPath, `<ctx:task name="implement-auth" action="start"/>`)
    
    // 2. Add working observations
    runStop(t, dbPath, `<ctx:remember type="observation" tags="tier:working">
Auth endpoint needs rate limiting.
</ctx:remember>`)
    
    // 3. Make a decision
    runStop(t, dbPath, `<ctx:remember type="decision" tags="tier:reference">
Using token bucket algorithm for rate limiting.
</ctx:remember>`)
    
    // 4. End task with summarize
    runStop(t, dbPath, `<ctx:task name="implement-auth" action="end" summarize="true"/>`)
    
    // 5. Verify states
    nodes := listNodes(t, dbPath)
    
    var decision, observation *Node
    for _, n := range nodes {
        if strings.Contains(n.Content, "token bucket") {
            decision = &n
        }
        if strings.Contains(n.Content, "rate limiting") && n.Type == "observation" {
            observation = &n
        }
    }
    
    // Decision stays in reference
    assert.Contains(t, decision.Tags, "tier:reference")
    
    // Observation archived
    assert.Contains(t, observation.Tags, "tier:off-context")
    assert.Contains(t, observation.Tags, "task:implement-auth")
}

func TestE2E_ExpandSummary(t *testing.T) {
    dbPath := t.TempDir() + "/test.db"
    
    // Create and summarize nodes
    for i := 0; i < 2; i++ {
        runStop(t, dbPath, fmt.Sprintf(`<ctx:remember type="observation" tags="tier:working">Detail %d</ctx:remember>`, i))
    }
    
    nodes := listNodes(t, dbPath)
    ids := []string{nodes[0].ID, nodes[1].ID}
    
    runStop(t, dbPath, fmt.Sprintf(`<ctx:summarize nodes="%s" archive="true">Summary of details</ctx:summarize>`, strings.Join(ids, ",")))
    
    // Get summary ID
    nodes = listNodes(t, dbPath)
    var summaryID string
    for _, n := range nodes {
        if n.Type == "summary" {
            summaryID = n.ID
            break
        }
    }
    
    // Request expansion
    runStop(t, dbPath, fmt.Sprintf(`<ctx:expand node="%s"/>`, summaryID))
    
    // Next prompt should include expanded sources
    promptOutput := runPromptSubmit(t, dbPath, "continue")
    assert.Contains(t, promptOutput.AdditionalContext, "Detail 0")
    assert.Contains(t, promptOutput.AdditionalContext, "Detail 1")
}
```

---

## Test Utilities

**File**: `testutil/testutil.go`

```go
// testutil/testutil.go

package testutil

import (
    "encoding/json"
    "os/exec"
    "strings"
    "testing"
    
    "github.com/stretchr/testify/require"
)

// SetupTestDB creates a test database and returns the path
func SetupTestDB(t *testing.T) (*db.DB, string) {
    t.Helper()
    path := t.TempDir() + "/test.db"
    database, err := db.Open(path)
    require.NoError(t, err)
    t.Cleanup(func() { database.Close() })
    return database, path
}

// RunCtx executes ctx with arguments and returns stdout
func RunCtx(t *testing.T, args ...string) string {
    t.Helper()
    cmd := exec.Command("ctx", args...)
    output, err := cmd.CombinedOutput()
    require.NoError(t, err, "ctx %v failed: %s", args, string(output))
    return string(output)
}

// RunCtxExpectError executes ctx expecting an error
func RunCtxExpectError(t *testing.T, args ...string) string {
    t.Helper()
    cmd := exec.Command("ctx", args...)
    output, err := cmd.CombinedOutput()
    require.Error(t, err)
    return string(output)
}

// HookOutput represents the JSON output from hooks
type HookOutput struct {
    HookSpecificOutput struct {
        HookEventName     string `json:"hookEventName"`
        AdditionalContext string `json:"additionalContext"`
    } `json:"hookSpecificOutput"`
}

// RunSessionStart simulates a SessionStart hook call
func RunSessionStart(t *testing.T, dbPath string) HookOutput {
    t.Helper()
    
    input := `{"session_id": "test", "hook_event_name": "SessionStart", "source": "startup"}`
    
    cmd := exec.Command("ctx", "--db", dbPath, "hook", "session-start")
    cmd.Stdin = strings.NewReader(input)
    
    output, err := cmd.Output()
    require.NoError(t, err)
    
    var result HookOutput
    json.Unmarshal(output, &result)
    return result
}

// RunStop simulates a Stop hook call with Claude's response
func RunStop(t *testing.T, dbPath, response string) {
    t.Helper()
    
    cmd := exec.Command("ctx", "--db", dbPath, "hook", "stop", "--response", response)
    cmd.Stdin = strings.NewReader(`{"hook_event_name": "Stop"}`)
    
    err := cmd.Run()
    require.NoError(t, err)
}

// RunPromptSubmit simulates a UserPromptSubmit hook call
func RunPromptSubmit(t *testing.T, dbPath, prompt string) HookOutput {
    t.Helper()
    
    input := fmt.Sprintf(`{"hook_event_name": "UserPromptSubmit", "prompt": "%s"}`, prompt)
    
    cmd := exec.Command("ctx", "--db", dbPath, "hook", "prompt-submit")
    cmd.Stdin = strings.NewReader(input)
    
    output, err := cmd.Output()
    require.NoError(t, err)
    
    var result HookOutput
    json.Unmarshal(output, &result)
    return result
}

// ListNodes returns all nodes in the database
func ListNodes(t *testing.T, dbPath string) []Node {
    t.Helper()
    
    output := RunCtx(t, "--db", dbPath, "list", "--format", "json")
    
    var nodes []Node
    json.Unmarshal([]byte(output), &nodes)
    return nodes
}

// GetNode returns a specific node
func GetNode(t *testing.T, dbPath, id string) Node {
    t.Helper()
    
    output := RunCtx(t, "--db", dbPath, "show", id, "--format", "json")
    
    var node Node
    json.Unmarshal([]byte(output), &node)
    return node
}

// GetEdges returns edges for a node
func GetEdges(t *testing.T, dbPath, id string) []Edge {
    t.Helper()
    
    output := RunCtx(t, "--db", dbPath, "edges", id, "--format", "json")
    
    var edges []Edge
    json.Unmarshal([]byte(output), &edges)
    return edges
}

// ExtractID extracts the node ID from "Added: <id>" output
func ExtractID(output string) string {
    // Parse "Added: 01HQ..." format
    parts := strings.Split(output, "Added: ")
    if len(parts) < 2 {
        return ""
    }
    return strings.TrimSpace(strings.Split(parts[1], "\n")[0])
}

// Ptr returns a pointer to the value
func Ptr[T any](v T) *T {
    return &v
}
```

---

## Test Data Files

### testdata/responses/single_remember.txt

```
Here's my analysis of the authentication system.

<ctx:remember type="fact" tags="project:auth,tier:reference">
The API uses OAuth 2.0 with PKCE for public clients. Refresh tokens are stored server-side only for security.
</ctx:remember>

Let me know if you need more details about the implementation.
```

### testdata/responses/single_remember.golden.json

```json
[
  {
    "type": "remember",
    "attrs": {
      "type": "fact",
      "tags": "project:auth,tier:reference"
    },
    "content": "The API uses OAuth 2.0 with PKCE for public clients. Refresh tokens are stored server-side only for security."
  }
]
```

### testdata/responses/multi_command.txt

```
I've analyzed the codebase and here are my findings:

<ctx:remember type="fact" tags="tier:reference">
The database uses SQLite with WAL mode enabled.
</ctx:remember>

Based on our discussion, I'm recording this decision:

<ctx:remember type="decision" tags="tier:reference">
We chose SQLite over PostgreSQL for single-binary deployment simplicity.
</ctx:remember>

<ctx:link from="01HQ1234" to="01HQ5678" type="DEPENDS_ON"/>

<ctx:status/>
```

### testdata/responses/code_block_ignore.txt

```
Here's how you would use the ctx commands:

```xml
<ctx:remember type="fact">This should be ignored</ctx:remember>
```

And inline: `<ctx:recall query="type:fact"/>` is how you recall.

<ctx:remember type="fact">This one is real and should be parsed</ctx:remember>
```

---

## Makefile

```makefile
.PHONY: test test-unit test-integration test-e2e test-coverage test-fuzz lint build

# Default test target
test: test-unit test-integration

# Unit tests only (fast)
test-unit:
	go test -v -short ./internal/...

# Integration tests (requires built binary)
test-integration: build
	go test -v -run Integration ./cmd/...

# End-to-end tests
test-e2e: build
	go test -v ./e2e/...

# All tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Fuzz testing (run for specified time)
test-fuzz:
	go test -fuzz=FuzzQueryParser -fuzztime=30s ./internal/query/

# Update golden files
test-golden-update:
	go test ./internal/hook/... -update

# Lint
lint:
	golangci-lint run

# Build
build:
	go build -o ctx .

# Clean
clean:
	rm -f ctx coverage.out coverage.html
```

---

## CI Configuration

**.github/workflows/test.yml**

```yaml
name: Test

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      
      - name: Install dependencies
        run: go mod download
      
      - name: Lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: latest
      
      - name: Unit tests
        run: make test-unit
      
      - name: Build binary
        run: make build
      
      - name: Integration tests
        run: make test-integration
      
      - name: E2E tests
        run: make test-e2e
      
      - name: Coverage
        run: |
          go test -coverprofile=coverage.out ./...
          go tool cover -func=coverage.out
      
      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: coverage.out

  fuzz:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      
      - name: Fuzz query parser
        run: go test -fuzz=FuzzQueryParser -fuzztime=60s ./internal/query/
```

---

## Edge Cases Checklist

| Category | Edge Case | Test Location |
|----------|-----------|---------------|
| Database | Corrupt db file | `db_test.go` |
| Database | Concurrent writes | `db_test.go` |
| Database | Very long content | `nodes_test.go` |
| Database | Empty content | `nodes_test.go` |
| Query | Empty query string | `parser_test.go` |
| Query | Very long query | `parser_test.go` |
| Query | Special characters | `parser_test.go` |
| Query | No matches | `executor_test.go` |
| Parser | Nested tags | `hook/parser_test.go` |
| Parser | Unclosed tags | `hook/parser_test.go` |
| Parser | Tags in code blocks | `hook/parser_test.go` |
| Parser | Unicode content | `hook/parser_test.go` |
| Parser | HTML entities | `hook/parser_test.go` |
| Budget | Zero budget | `composer_test.go` |
| Budget | Single node exceeds | `composer_test.go` |
| Hooks | Invalid JSON input | `hook_test.go` |
| Hooks | Empty response | `hook_test.go` |
| CLI | No db file | `cmd_test.go` |
| CLI | Missing required flags | `cmd_test.go` |
| CLI | Invalid node ID | `cmd_test.go` |
