import type { CodexPlanStep } from "../../services/codexConversation";

export interface PlanTimelineItem {
  type: "plan";
  id: string;
  timestamp?: string;
  explanation?: string;
  steps: CodexPlanStep[];
}
