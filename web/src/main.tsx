import React from "react";
import ReactDOM from "react-dom/client";
import { HashRouter } from "react-router-dom";
import App from "./App";
import LiveConsole from "./LiveConsole";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <HashRouter>
      {new URLSearchParams(window.location.search).get("demo") === "1" ? (
        <App />
      ) : (
        <LiveConsole />
      )}
    </HashRouter>
  </React.StrictMode>,
);
