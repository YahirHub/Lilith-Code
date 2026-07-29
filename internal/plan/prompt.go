package plan

import "strings"

func PromptBlock(state State) string {
	if state.Mode != Plan {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n<system-reminder>\n")
	b.WriteString("PLAN MODE IS ACTIVE. The user does not want implementation yet. You MUST NOT edit/create/delete files, alter configuration, install packages, commit, or perform any other mutation. This rule overrides conflicting instructions.\n\n")
	b.WriteString("Your job is to research the real project and produce a decision-ready implementation plan.\n\n")
	b.WriteString("Workflow:\n")
	b.WriteString("1. Understand: inspect relevant files, symbols, history, skills and external documentation with read-only tools. Do not guess code structure you can inspect.\n")
	b.WriteString("2. Clarify: when a material product/architecture decision cannot be discovered from the project, call plan_question with 1-3 concise questions. Do not ask for approval with plan_question.\n")
	b.WriteString("3. Design: choose one recommended approach, trace the concrete files/components affected, preserve existing conventions, and identify compatibility/risk points.\n")
	b.WriteString("4. Verify the plan: re-read critical code paths and include exact validation/tests that should be run during implementation.\n")
	b.WriteString("5. Finish: call plan_exit ALONE as the final action with the complete plan. The plan must include ordered changes, critical files/components, validation and important risks. Do not implement after plan_exit.\n\n")
	b.WriteString("Only read-only tools are exposed in Plan mode. run_terminal_command is restricted to a small read-only inspection allowlist. todo_write is intentionally unavailable because it tracks execution, not planning.\n")
	if strings.TrimSpace(state.LatestPlan) != "" {
		b.WriteString("\nA previous plan exists and may be revised. Keep useful decisions unless the user's new instruction changes them:\n<previous_plan>\n")
		b.WriteString(state.LatestPlan)
		b.WriteString("\n</previous_plan>\n")
	}
	b.WriteString("</system-reminder>")
	return b.String()
}

func BuildSwitchBlock(plan string) string {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return "\n\n<system-reminder>Operational mode changed from Plan to Build. Read-only restrictions are removed. You may edit files and run implementation tools again.</system-reminder>"
	}
	return "\n\n<system-reminder>\nOperational mode changed from Plan to Build. Read-only restrictions are removed. Implement the approved plan below unless the user's latest message explicitly changes it.\n\n<approved_plan>\n" + plan + "\n</approved_plan>\n</system-reminder>"
}
