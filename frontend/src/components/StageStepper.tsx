import { Badge, Spinner, Tooltip } from "@fluentui/react-components";
import {
  CheckmarkCircle20Filled,
  Clock20Regular,
  DismissCircle20Filled,
  PauseCircle20Filled,
  SubtractCircle20Regular,
} from "@fluentui/react-icons";
import type { StageExecution } from "../types/dragon.js";
import { formatDurationMs } from "../types/dragon.js";

interface StageStepperProps {
  stages: StageExecution[];
}

export function StageStepper({ stages }: StageStepperProps) {
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(${Math.max(stages.length, 1)}, minmax(110px, 1fr))`,
        gap: 8,
      }}
    >
      {stages.map((st) => {
        const isRunning = st.status === "running";
        const isWaiting = st.status === "waiting_approval";
        const isDone = st.status === "succeeded" || st.status === "approved";
        const isFailed = st.status === "failed";
        const isSkipped = st.status === "skipped";

        let borderColor = "var(--dragon-stroke)";
        if (isRunning) borderColor = "#0F6CBD";
        if (isWaiting) borderColor = "#D97706";
        if (isDone) borderColor = "rgba(16, 185, 129, 0.45)";
        if (isFailed) borderColor = "rgba(239, 68, 68, 0.55)";

        return (
          <Tooltip
            key={st.id}
            content={st.errorMessage || st.summary || `${st.stageName} (${st.status})`}
            relationship="label"
          >
            <div
              className="dragon-subtle-box"
              style={{
                padding: "10px 10px",
                borderColor,
                opacity: isSkipped ? 0.55 : 1,
                display: "flex",
                flexDirection: "column",
                gap: 6,
              }}
            >
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
                <span style={{ display: "inline-flex", alignItems: "center" }}>
                  {isRunning && <Spinner size="extra-tiny" />}
                  {isWaiting && <PauseCircle20Filled style={{ color: "#D97706" }} />}
                  {isDone && <CheckmarkCircle20Filled style={{ color: "#10B981" }} />}
                  {isFailed && <DismissCircle20Filled style={{ color: "#EF4444" }} />}
                  {isSkipped && <SubtractCircle20Regular style={{ color: "var(--dragon-text-secondary)" }} />}
                  {!isRunning && !isWaiting && !isDone && !isFailed && !isSkipped && (
                    <Clock20Regular style={{ color: "var(--dragon-text-secondary)" }} />
                  )}
                </span>
                <span style={{ fontSize: 11, color: "var(--dragon-text-secondary)" }} className="tabular-nums">
                  {st.durationMs > 0 ? formatDurationMs(st.durationMs) : `#${st.orderIndex}`}
                </span>
              </div>

              <div style={{ fontSize: 12.5, fontWeight: 600, lineHeight: 1.25 }}>{st.stageName}</div>

              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 4, flexWrap: "wrap" }}>
                <Badge appearance="outline" size="extra-small" color="informative">
                  {st.agentId || "auto"}
                </Badge>
                {st.modelRef && (
                  <span
                    style={{
                      fontSize: 10,
                      color: "var(--dragon-text-secondary)",
                      maxWidth: 92,
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {st.modelRef.includes("/") ? st.modelRef.split("/")[1] : st.modelRef}
                  </span>
                )}
                {st.attempt > 1 && (
                  <span style={{ fontSize: 10.5, color: "#D97706" }}>x{st.attempt}</span>
                )}
              </div>
            </div>
          </Tooltip>
        );
      })}
    </div>
  );
}
