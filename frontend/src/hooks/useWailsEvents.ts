import { useEffect } from "react";
import { Events } from "@wailsio/runtime";
import {
  DragonIssuesSynced,
  DragonLogChunk,
  DragonNavigate,
  DragonReleaseUpdated,
  DragonRunUpdated,
  DragonStageUpdated,
} from "../../bindings/events/index.js";
import type {
  NavigateEvent,
  PipelineRun,
  ReleaseRecord,
  RunLog,
  StageExecution,
  SyncSummary,
} from "../types/dragon.js";

export interface WailsEventHandlers {
  onRunUpdated?: (run: PipelineRun) => void;
  onStageUpdated?: (stage: StageExecution) => void;
  onLogChunk?: (log: RunLog) => void;
  onIssuesSynced?: (summary: SyncSummary) => void;
  onReleaseUpdated?: (rec: ReleaseRecord) => void;
  onNavigate?: (nav: NavigateEvent) => void;
}

export function useWailsEvents(handlers: WailsEventHandlers) {
  useEffect(() => {
    const unsubs: Array<() => void> = [];

    if (handlers.onRunUpdated) {
      unsubs.push(
        Events.On(DragonRunUpdated, (ev) => {
          if (ev.data) handlers.onRunUpdated?.(ev.data as PipelineRun);
        })
      );
    }
    if (handlers.onStageUpdated) {
      unsubs.push(
        Events.On(DragonStageUpdated, (ev) => {
          if (ev.data) handlers.onStageUpdated?.(ev.data as StageExecution);
        })
      );
    }
    if (handlers.onLogChunk) {
      unsubs.push(
        Events.On(DragonLogChunk, (ev) => {
          if (ev.data) handlers.onLogChunk?.(ev.data as RunLog);
        })
      );
    }
    if (handlers.onIssuesSynced) {
      unsubs.push(
        Events.On(DragonIssuesSynced, (ev) => {
          if (ev.data) handlers.onIssuesSynced?.(ev.data as SyncSummary);
        })
      );
    }
    if (handlers.onReleaseUpdated) {
      unsubs.push(
        Events.On(DragonReleaseUpdated, (ev) => {
          if (ev.data) handlers.onReleaseUpdated?.(ev.data as ReleaseRecord);
        })
      );
    }
    if (handlers.onNavigate) {
      unsubs.push(
        Events.On(DragonNavigate, (ev) => {
          if (ev.data) handlers.onNavigate?.(ev.data as NavigateEvent);
        })
      );
    }

    return () => {
      for (const unsub of unsubs) {
        unsub();
      }
    };
  }, [
    handlers.onRunUpdated,
    handlers.onStageUpdated,
    handlers.onLogChunk,
    handlers.onIssuesSynced,
    handlers.onReleaseUpdated,
    handlers.onNavigate,
  ]);
}
