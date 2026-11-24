"use client";

import { Component } from "react";
import type { ComponentType, ReactNode } from "react";
import { FallBackModel } from "./FallBackModel";
import type { Stage } from "./types";

type Props = { stage: Stage; children: ReactNode; fallback?: ComponentType<{ stage: Stage }> };
type State = { failed: boolean };

export class ModelBoundary extends Component<Props, State> {
  state: State = { failed: false };

  static getDerivedStateFromError(): State {
    return { failed: true };
  }

  render() {
    if (this.state.failed) {
      const Fallback = this.props.fallback ?? FallBackModel;
      return <Fallback stage={this.props.stage} />;
    }
    return this.props.children;
  }
}
