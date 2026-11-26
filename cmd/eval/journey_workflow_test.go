package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

const journeyReferenceRecipe = `{
"version":4,"name":"Ledger review",
"inputs":{"amounts":{"type":"array","required":true},"report_name":{"type":"string","required":true}},
"steps":[
{"id":"check","tool":"run_program","args":{"program":"go","args":["test","./..."],"workdir":"."}},
{"id":"arguments","tool":"script_transform","depends_on":["check"],"args":{"input":{"$input":"amounts"},"code":"return ['run', './cmd/ledger'].concat(input);"}},
{"id":"total","tool":"run_program","args":{"program":"go","args":{"$step":"arguments"},"workdir":"."}},
{"id":"text","tool":"artifact_read_text","args":{"id":{"$step":"total","pointer":"/stdout/id"}}},
{"id":"publish","tool":"artifact_write","review":true,"args":{"name":{"$input":"report_name"},"text":{"$step":"text","pointer":"/text"}}}
],"outputs":{"report":{"$step":"publish"}}}`

// This implementation is only test scaffolding for the workflow oracle. The live
// journey starts from fixtures-ledger's broken source and uses ledger.gotest.
const journeyReferenceLedger = `package ledger
import ("fmt"; "strconv"; "strings")
func TotalCents(values []string) (int,error) {
 total:=0
 for _,value:=range values {
  value=strings.TrimSpace(value); sign:=1
  if strings.HasPrefix(value,"-"){ sign=-1;value=value[1:] } else if strings.HasPrefix(value,"+"){ value=value[1:] }
  parts:=strings.Split(value,".")
  if len(parts)>2 || parts[0]=="" {return 0,fmt.Errorf("invalid amount")}
  fraction:="00"
  if len(parts)==2 {if len(parts[1])<1 || len(parts[1])>2 {return 0,fmt.Errorf("invalid fraction")}; fraction=(parts[1]+"0")[:2]}
  for _,c:=range parts[0]+fraction {if c<'0'||c>'9' {return 0,fmt.Errorf("invalid digit")}}
  whole,err:=strconv.Atoi(parts[0]);if err!=nil{return 0,err}
  cents,err:=strconv.Atoi(fraction);if err!=nil{return 0,err}
  total+=sign*(whole*100+cents)
 }
 return total,nil
}`

func journeyWorkflowWorkspace(t *testing.T, recipe string) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := ws.Close()
		if err != nil {
			t.Error(err)
		}
	})
	err = ws.MkdirAll("cmd/ledger", 0700)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":    "module example.com/ledger\n\ngo 1.26\n",
		"ledger.go": journeyReferenceLedger,
		"ledger_test.go": `package ledger
import "testing"
func TestSmoke(t *testing.T) {n,err:=TotalCents([]string{"1.2"});if err!=nil||n!=120 {t.Fatal(n,err)}}`,
		"cmd/ledger/main.go": `package main
import ("fmt";"os";"example.com/ledger")
func main(){n,err:=ledger.TotalCents(os.Args[1:]);if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)};fmt.Println(n)}`,
		"ledger-review.json": recipe,
	}
	for path, body := range files {
		err = ws.WriteFile(path, []byte(body), 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func TestJourneyWorkflowOracle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		from    string
		to      string
		want    string
		reorder bool
	}{
		{name: "valid"},
		{name: "dependency-order", reorder: true},
		{name: "missing-review", from: `"review":true`, to: `"review":false`, want: "before approval"},
		{name: "wrong-report", from: `"text":{"$step":"text","pointer":"/text"}`, to: `"text":"42\n"`, want: "report must contain"},
		{name: "ignores-inputs", from: `"input":{"$input":"amounts"}`, to: `"input":["1.23"]`, want: "actual CLI invocation"},
		{name: "skips-checks", from: `"args":["test","./..."]`, to: `"args":["version"]`, want: "successful project checks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recipe := journeyReferenceRecipe
			if tc.from != "" {
				recipe = strings.Replace(recipe, tc.from, tc.to, 1)
			}
			if tc.reorder {
				var definition workflow.Definition
				err := json.Unmarshal([]byte(recipe), &definition)
				if err != nil {
					t.Fatal(err)
				}
				definition.Steps = append(definition.Steps[1:], definition.Steps[0])
				data, err := json.Marshal(definition)
				if err != nil {
					t.Fatal(err)
				}
				recipe = string(data)
			}
			ws := journeyWorkflowWorkspace(t, recipe)
			evidence, err := checkJourneyWorkflow(t.Context(), ws)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want rejection %q, got %v", tc.want, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(evidence.Runs) != 3 || evidence.Runs[0].ReportText != "1446\n" || evidence.Runs[1].ReportText != "-630\n" || evidence.Runs[2].Status != "failed" {
				t.Fatalf("incomplete evidence: %+v", evidence)
			}
		})
	}
}

func TestJourneyWorkflowRejectsFailingChecks(t *testing.T) {
	ws := journeyWorkflowWorkspace(t, journeyReferenceRecipe)
	err := ws.WriteFile("failing_test.go", []byte("package ledger\nimport \"testing\"\nfunc TestBroken(t *testing.T){t.Fatal(\"broken\")}\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = checkJourneyWorkflow(t.Context(), ws)
	if err == nil {
		t.Fatal("accepted a workflow whose project checks fail")
	}
}
