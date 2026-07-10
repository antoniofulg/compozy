package main

import (
	"context"
	"strings"
	"testing"

	extension "github.com/compozy/compozy/sdk/extension"
)

type fakeTaskClient struct {
	tasks   []extension.Task
	creates []extension.TaskCreateRequest
}

func (f *fakeTaskClient) List(
	context.Context,
	extension.TaskListRequest,
) ([]extension.Task, error) {
	return append([]extension.Task(nil), f.tasks...), nil
}

func (f *fakeTaskClient) Create(
	_ context.Context,
	req extension.TaskCreateRequest,
) (*extension.Task, error) {
	f.creates = append(f.creates, req)
	number := len(f.tasks) + 1
	task := extension.Task{
		Workflow:     req.Workflow,
		Number:       number,
		Path:         ".compozy/tasks/" + req.Workflow + "/task_99.md",
		Status:       req.Frontmatter.Status,
		Title:        req.Title,
		Type:         req.Frontmatter.Type,
		Complexity:   req.Frontmatter.Complexity,
		Dependencies: append([]string(nil), req.Frontmatter.Dependencies...),
		Body:         req.Body,
	}
	f.tasks = append(f.tasks, task)
	return &task, nil
}

func TestEnsureQATasksCreatesReportAndExecutionTasks(t *testing.T) {
	t.Parallel()

	client := &fakeTaskClient{tasks: []extension.Task{
		{Number: 2, Title: "Second", Type: "backend"},
		{Number: 1, Title: "First", Type: "backend"},
	}}

	if err := ensureQATasks(context.Background(), client, "daemon-web-ui"); err != nil {
		t.Fatalf("ensureQATasks() error = %v", err)
	}
	if len(client.creates) != 2 {
		t.Fatalf("creates = %d, want 2", len(client.creates))
	}

	report := client.creates[0]
	if report.Title != "Daemon Web Ui QA plan and regression artifacts" {
		t.Fatalf("report.Title = %q", report.Title)
	}
	if report.Frontmatter.Type != qaReportTaskType || report.Frontmatter.Complexity != qaReportComplexity {
		t.Fatalf("report frontmatter = %#v", report.Frontmatter)
	}
	if !report.UpdateIndex {
		t.Fatal("report.UpdateIndex = false, want true")
	}
	if got := strings.Join(report.Frontmatter.Dependencies, ","); got != "task_01,task_02" {
		t.Fatalf("report dependencies = %q, want task_01,task_02", got)
	}
	if !strings.Contains(report.Body, reportMarker) ||
		!strings.Contains(report.Body, "qa-output-path=.compozy/tasks/daemon-web-ui") {
		t.Fatalf("report body missing marker or output path: %q", report.Body)
	}

	execution := client.creates[1]
	if execution.Frontmatter.Type != qaExecutionTaskType ||
		execution.Frontmatter.Complexity != qaExecutionComplexity {
		t.Fatalf("execution frontmatter = %#v", execution.Frontmatter)
	}
	if got := strings.Join(execution.Frontmatter.Dependencies, ","); got != "task_03" {
		t.Fatalf("execution dependencies = %q, want task_03", got)
	}
	if !strings.Contains(execution.Body, executionMarker) ||
		!strings.Contains(execution.Body, "/qa-execution") {
		t.Fatalf("execution body missing marker or skill: %q", execution.Body)
	}
}

func TestEnsureQATasksIsIdempotentForExistingQATasks(t *testing.T) {
	t.Parallel()

	client := &fakeTaskClient{tasks: []extension.Task{
		{Number: 1, Title: "Implementation", Type: "backend"},
		{Number: 2, Title: "Daemon QA plan and regression artifacts", Type: "docs"},
		{Number: 3, Title: "Daemon QA execution and operator-flow validation", Type: "test"},
	}}

	if err := ensureQATasks(context.Background(), client, "daemon"); err != nil {
		t.Fatalf("ensureQATasks() error = %v", err)
	}
	if len(client.creates) != 0 {
		t.Fatalf("creates = %#v, want none", client.creates)
	}
}

func TestRuntimeForTaskSelectsQARuntimes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		task extension.TaskRuntimeTask
		want extension.TaskRuntime
		ok   bool
	}{
		{
			name: "report",
			task: extension.TaskRuntimeTask{Title: "Daemon QA plan and regression artifacts", Type: "docs"},
			want: extension.TaskRuntime{IDE: "claude", Model: "opus", ReasoningEffort: "xhigh"},
			ok:   true,
		},
		{
			name: "execution",
			task: extension.TaskRuntimeTask{Title: "Daemon QA execution and operator-flow validation", Type: "test"},
			want: extension.TaskRuntime{IDE: "codex", Model: "gpt-5.6-sol", ReasoningEffort: "xhigh"},
			ok:   true,
		},
		{
			name: "unrelated",
			task: extension.TaskRuntimeTask{Title: "Implementation", Type: "backend"},
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := runtimeForTask(tc.task)
			if ok != tc.ok {
				t.Fatalf("ok = %t, want %t", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("runtime = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestMutateSessionRequestPrefixesGoalForQAExecution(t *testing.T) {
	t.Parallel()

	originalPrompt := executionMarker + "\n\nRun the QA execution task."
	request, changed := mutateSessionRequest(extension.SessionRequest{
		Prompt: []byte(originalPrompt),
	})
	if !changed {
		t.Fatal("changed = false, want true")
	}
	prompt := string(request.Prompt)
	if !strings.HasPrefix(prompt, "/goal ") {
		t.Fatalf("prompt prefix = %q, want /goal", prompt[:min(len(prompt), 12)])
	}
	if !strings.Contains(prompt, originalPrompt) {
		t.Fatalf("prompt = %q, want original prompt preserved", prompt)
	}
}

func TestMutateSessionRequestDoesNotDuplicateExistingGoal(t *testing.T) {
	t.Parallel()

	request, changed := mutateSessionRequest(extension.SessionRequest{
		Prompt: []byte(" \n\t/goal Existing goal\n\n" + executionMarker),
	})
	if !changed {
		t.Fatal("changed = false, want true")
	}
	prompt := string(request.Prompt)
	if strings.Count(prompt, "/goal") != 1 {
		t.Fatalf("prompt = %q, want one /goal", prompt)
	}
	if !strings.HasPrefix(prompt, "/goal Existing goal") {
		t.Fatalf("prompt = %q, want trimmed existing goal first", prompt)
	}
}

func TestMutateSessionRequestSetsClaudeEffortForQAReport(t *testing.T) {
	t.Parallel()

	request, changed := mutateSessionRequest(extension.SessionRequest{
		Prompt:   []byte(reportMarker + "\n\nPlan QA."),
		ExtraEnv: map[string]string{"KEEP": "value"},
	})
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if got := request.ExtraEnv[claudeEffortEnv]; got != defaultReasoningEffort {
		t.Fatalf("%s = %q, want %q", claudeEffortEnv, got, defaultReasoningEffort)
	}
	if got := request.ExtraEnv["KEEP"]; got != "value" {
		t.Fatalf("KEEP = %q, want value", got)
	}
}

func TestRuntimeForTaskHonorsEnvOverrides(t *testing.T) {
	reportTask := extension.TaskRuntimeTask{
		Title: "Demo QA plan and regression artifacts",
		Type:  qaReportTaskType,
	}
	executionTask := extension.TaskRuntimeTask{
		Title: "Demo QA execution and operator-flow validation",
		Type:  qaExecutionTaskType,
	}

	tests := []struct {
		name       string
		env        map[string]string
		task       extension.TaskRuntimeTask
		wantIDE    string
		wantModel  string
		wantEffort string
	}{
		{
			name:       "execution defaults when unset",
			task:       executionTask,
			wantIDE:    defaultQAExecutionIDE,
			wantModel:  defaultQAExecutionModel,
			wantEffort: defaultReasoningEffort,
		},
		{
			name:       "execution IDE override drops the default model",
			env:        map[string]string{qaExecutionIDEEnv: "claude"},
			task:       executionTask,
			wantIDE:    "claude",
			wantModel:  "",
			wantEffort: defaultReasoningEffort,
		},
		{
			name:       "execution IDE and model override",
			env:        map[string]string{qaExecutionIDEEnv: "claude", qaExecutionModelEnv: "opus"},
			task:       executionTask,
			wantIDE:    "claude",
			wantModel:  "opus",
			wantEffort: defaultReasoningEffort,
		},
		{
			name:       "execution model override keeps default IDE",
			env:        map[string]string{qaExecutionModelEnv: "custom-model"},
			task:       executionTask,
			wantIDE:    defaultQAExecutionIDE,
			wantModel:  "custom-model",
			wantEffort: defaultReasoningEffort,
		},
		{
			name:       "execution reasoning effort override",
			env:        map[string]string{qaExecutionEffortEnv: "medium"},
			task:       executionTask,
			wantIDE:    defaultQAExecutionIDE,
			wantModel:  defaultQAExecutionModel,
			wantEffort: "medium",
		},
		{
			name:       "report defaults when unset",
			task:       reportTask,
			wantIDE:    defaultQAReportIDE,
			wantModel:  defaultQAReportModel,
			wantEffort: defaultReasoningEffort,
		},
		{
			name:       "report IDE override drops the default model",
			env:        map[string]string{qaReportIDEEnv: "codex"},
			task:       reportTask,
			wantIDE:    "codex",
			wantModel:  "",
			wantEffort: defaultReasoningEffort,
		},
		{
			name:       "blank env value falls back to default",
			env:        map[string]string{qaExecutionIDEEnv: "   "},
			task:       executionTask,
			wantIDE:    defaultQAExecutionIDE,
			wantModel:  defaultQAExecutionModel,
			wantEffort: defaultReasoningEffort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv restores the environment after each subtest, so cases
			// with an empty env map observe the compiled-in defaults.
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			got, ok := runtimeForTask(tt.task)
			if !ok {
				t.Fatalf("runtimeForTask(%q) ok = false, want true", tt.task.Title)
			}
			if got.IDE != tt.wantIDE || got.Model != tt.wantModel || got.ReasoningEffort != tt.wantEffort {
				t.Fatalf(
					"runtimeForTask() = {IDE:%q Model:%q Effort:%q}, want {IDE:%q Model:%q Effort:%q}",
					got.IDE, got.Model, got.ReasoningEffort, tt.wantIDE, tt.wantModel, tt.wantEffort,
				)
			}
		})
	}
}
