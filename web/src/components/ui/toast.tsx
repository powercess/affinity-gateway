import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Toast } from "radix-ui";
import { CheckCircle2, CircleAlert, X } from "lucide-react";
import { Button } from "./button";

type Notice = { id: number; message: string; kind: "success" | "error" };

export function useNotifications() {
  const [notices, setNotices] = useState<Notice[]>([]);
  const nextID = useRef(0);
  function show(kind: Notice["kind"], message: string) {
    const notice = { id: ++nextID.current, kind, message };
    setNotices((current) => [...current.slice(-3), notice]);
  }
  return {
    notify: {
      success: (message: string) => show("success", message),
      error: (message: string) => show("error", message),
    },
    toaster: createPortal(
      <Toast.Provider label="通知" swipeDirection="right">
        {notices.map((notice) => (
          <Toast.Root
            key={notice.id}
            className="app-toast"
            data-kind={notice.kind}
            duration={notice.kind === "error" ? 8000 : 3000}
            onOpenChange={(open) => {
              if (!open) setNotices((current) => current.filter((item) => item.id !== notice.id));
            }}
          >
            {notice.kind === "success" ? <CheckCircle2 aria-hidden="true" size={20} /> : <CircleAlert aria-hidden="true" size={20} />}
            <Toast.Description className="app-toast-message">{notice.message}</Toast.Description>
            <Toast.Close asChild><Button variant="ghost" size="icon-sm" aria-label="关闭通知"><X /></Button></Toast.Close>
          </Toast.Root>
        ))}
        <Toast.Viewport className="app-toast-viewport" label="通知（{hotkey}）" />
      </Toast.Provider>,
      document.body,
    ),
  };
}
