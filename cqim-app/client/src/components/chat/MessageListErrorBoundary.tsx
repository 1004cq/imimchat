import { Component, type ReactNode } from 'react';
import { AlertTriangle } from 'lucide-react';

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
}

/** 仅包裹消息列表：单条坏消息或渲染异常不炸整页 */
export default class MessageListErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch(error: Error) {
    console.error('[MessageList] 渲染异常:', error);
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex flex-1 items-center justify-center px-6 py-10 text-center">
          <div className="max-w-xs space-y-2">
            <AlertTriangle className="mx-auto text-amber-500" size={28} />
            <p className="text-sm text-foreground">部分消息无法显示</p>
            <p className="text-xs text-muted-foreground">请返回会话列表后重新进入，或下拉刷新后再试。</p>
            <button
              type="button"
              className="mt-2 text-xs text-primary underline"
              onClick={() => this.setState({ hasError: false })}
            >
              重试加载消息
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

/** 单条坏消息降级为占位气泡，避免整列白屏 */
export class MessageItemErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch(error: Error) {
    console.error('[MessageItem] 渲染异常:', error);
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex justify-center py-2 px-4">
          <span className="text-[10px] text-muted-foreground bg-dove-warm-gray/60 px-3 py-1.5 rounded-full">
            🔒 该条消息无法显示
          </span>
        </div>
      );
    }
    return this.props.children;
  }
}
