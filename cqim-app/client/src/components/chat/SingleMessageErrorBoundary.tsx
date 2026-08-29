import { Component, type ReactNode } from 'react';

interface Props {
  messageId: string;
  children: ReactNode;
}

interface State {
  hasError: boolean;
}

/** 单条消息渲染失败时不拖垮整页列表 */
export default class SingleMessageErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch(error: Error) {
    console.error(`[MessageList] 消息 ${this.props.messageId} 渲染异常:`, error);
  }

  componentDidUpdate(prevProps: Props) {
    if (prevProps.messageId !== this.props.messageId && this.state.hasError) {
      this.setState({ hasError: false });
    }
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex justify-center px-4 py-2">
          <span className="text-xs text-muted-foreground bg-muted/60 px-3 py-1.5 rounded-full">
            此条消息无法显示
          </span>
        </div>
      );
    }
    return this.props.children;
  }
}
