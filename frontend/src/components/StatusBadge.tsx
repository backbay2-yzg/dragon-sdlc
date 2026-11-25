import { Badge } from "@fluentui/react-components";
import { STATUS_LABELS } from "../types/dragon.js";

interface StatusBadgeProps {
  status: string;
  size?: "small" | "medium" | "large";
}

export function StatusBadge({ status, size = "small" }: StatusBadgeProps) {
  const label = STATUS_LABELS[status] || status || "未知";
  let color: "brand" | "danger" | "important" | "informative" | "severe" | "subtle" | "success" | "warning" =
    "subtle";

  switch (status) {
    case "running":
    case "in_progress":
    case "pr_open":
      color = "brand";
      break;
    case "waiting_approval":
    case "queued":
      color = "warning";
      break;
    case "succeeded":
    case "completed":
    case "approved":
    case "published":
    case "completed_success":
    case "success":
      color = "success";
      break;
    case "failed":
    case "failure":
    case "completed_failure":
      color = "danger";
      break;
    case "cancelled":
    case "ignored":
    case "skipped":
      color = "subtle";
      break;
    case "backlog":
    case "pending":
    case "draft":
      color = "informative";
      break;
  }

  return (
    <Badge appearance="tint" color={color} size={size}>
      {label}
    </Badge>
  );
}
