import * as React from 'react';
import * as ToastPrimitive from '@radix-ui/react-toast';
import { AlertCircle, CheckCircle2, Info, X } from 'lucide-react';
import { cn } from '@/lib/utils';

export type ToastVariant = 'success' | 'error' | 'info';

export type ToastOptions = {
  title: string;
  description?: string;
  variant?: ToastVariant;
};

type ToastItem = Required<Pick<ToastOptions, 'title' | 'variant'>> & {
  id: number;
  description?: string;
};

type ToastContextValue = {
  toast: (options: ToastOptions) => void;
};

const ToastContext = React.createContext<ToastContextValue | null>(null);

const variantStyles: Record<ToastVariant, string> = {
  success: 'border-l-4 border-l-primary',
  error: 'border-l-4 border-l-destructive',
  info: 'border-l-4 border-l-border',
};

const variantIcons: Record<ToastVariant, typeof CheckCircle2> = {
  success: CheckCircle2,
  error: AlertCircle,
  info: Info,
};

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = React.useState<ToastItem[]>([]);
  const counter = React.useRef(0);

  const toast = React.useCallback((options: ToastOptions) => {
    counter.current += 1;
    const id = counter.current;
    setItems((prev) => [...prev, { id, title: options.title, description: options.description, variant: options.variant ?? 'info' }]);
  }, []);

  const remove = React.useCallback((id: number) => {
    setItems((prev) => prev.filter((item) => item.id !== id));
  }, []);

  return (
    <ToastContext.Provider value={{ toast }}>
      <ToastPrimitive.Provider swipeDirection="right" duration={4500}>
        {children}
        {items.map((item) => {
          const Icon = variantIcons[item.variant];
          return (
            <ToastPrimitive.Root
              key={item.id}
              defaultOpen
              onOpenChange={(open) => {
                if (!open) remove(item.id);
              }}
              className={cn(
                'flex items-start gap-3 rounded-lg border border-border bg-card p-4 text-card-foreground shadow-lg',
                variantStyles[item.variant],
              )}
            >
              <Icon
                size={18}
                className={cn(
                  'mt-0.5 shrink-0',
                  item.variant === 'success' && 'text-primary',
                  item.variant === 'error' && 'text-destructive',
                  item.variant === 'info' && 'text-muted-foreground',
                )}
              />
              <div className="min-w-0 flex-1">
                <ToastPrimitive.Title className="text-sm font-medium">{item.title}</ToastPrimitive.Title>
                {item.description ? (
                  <ToastPrimitive.Description className="mt-0.5 break-words text-xs text-muted-foreground">
                    {item.description}
                  </ToastPrimitive.Description>
                ) : null}
              </div>
              <ToastPrimitive.Close className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground">
                <X size={14} />
              </ToastPrimitive.Close>
            </ToastPrimitive.Root>
          );
        })}
        <ToastPrimitive.Viewport className="fixed bottom-0 right-0 z-[100] m-0 flex w-full max-w-sm list-none flex-col gap-2 p-4 outline-none" />
      </ToastPrimitive.Provider>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const context = React.useContext(ToastContext);
  if (!context) throw new Error('useToast must be used within ToastProvider');
  return context;
}
