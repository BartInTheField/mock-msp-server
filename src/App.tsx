import React, { useState, useEffect, useCallback } from "react"
import { useKeyboard, useRenderer } from "@opentui/react"
import type { ServerState, LogEntry, OcpiModule } from "./server"
import { pullModule } from "./server"

interface AppProps {
  url: string
  port: number
  state: ServerState
  logs: LogEntry[]
  onLog: (entry: LogEntry) => void
}

const MAX_LOGS = 100

type View =
  | { screen: "dashboard" }
  | { screen: "list"; module: OcpiModule }
  | { screen: "detail"; module: OcpiModule; key: string }

const MODULES: { id: OcpiModule; label: string; pullKey: string }[] = [
  { id: "locations", label: "Locations", pullKey: "1" },
  { id: "sessions", label: "Sessions", pullKey: "2" },
  { id: "cdrs", label: "CDRs", pullKey: "3" },
  { id: "tariffs", label: "Tariffs", pullKey: "4" },
  { id: "tokens", label: "Tokens", pullKey: "5" },
]

export function App({ url, port, state, logs, onLog }: AppProps) {
  const renderer = useRenderer()
  const [statusMsg, setStatusMsg] = useState("")
  const [view, setView] = useState<View>({ screen: "dashboard" })
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [, forceUpdate] = useState(0)

  useEffect(() => {
    const interval = setInterval(() => forceUpdate((n) => n + 1), 500)
    return () => clearInterval(interval)
  }, [])

  const doPull = useCallback(
    async (module: OcpiModule) => {
      setStatusMsg(`Pulling ${module}...`)
      const result = await pullModule(state, module, onLog)
      setStatusMsg(result)
    },
    [state, onLog]
  )

  const getEntries = useCallback(
    (module: OcpiModule): [string, any][] => {
      return Object.entries(state[module])
    },
    [state]
  )

  useKeyboard((key) => {
    // Global shortcuts
    if (key.name === "q" && !key.ctrl) {
      if (view.screen !== "dashboard") {
        setView({ screen: "dashboard" })
        setSelectedIndex(0)
      } else {
        renderer.destroy()
      }
      return
    }
    if (key.name === "escape") {
      if (view.screen === "detail") {
        setView({ screen: "list", module: view.module })
        setSelectedIndex(0)
      } else if (view.screen === "list") {
        setView({ screen: "dashboard" })
        setSelectedIndex(0)
      }
      return
    }

    // Dashboard shortcuts
    if (view.screen === "dashboard") {
      // Pull shortcuts 1-5
      for (const mod of MODULES) {
        if (key.name === mod.pullKey) {
          doPull(mod.id)
          return
        }
      }
      // Browse: navigate module list with j/k or up/down
      if (key.name === "j" || key.name === "down") {
        setSelectedIndex((i) => Math.min(MODULES.length - 1, i + 1))
        return
      }
      if (key.name === "k" || key.name === "up") {
        setSelectedIndex((i) => Math.max(0, i - 1))
        return
      }
      if (key.name === "return" || key.name === "enter") {
        const mod = MODULES[selectedIndex]
        if (mod) {
          setView({ screen: "list", module: mod.id })
          setSelectedIndex(0)
        }
        return
      }
      if (key.name === "c") {
        logs.length = 0
        setStatusMsg("Logs cleared")
        return
      }
      // Pull all with 'a'
      if (key.name === "a") {
        ;(async () => {
          setStatusMsg("Pulling all modules...")
          const results: string[] = []
          for (const mod of MODULES) {
            const r = await pullModule(state, mod.id, onLog)
            results.push(r)
          }
          setStatusMsg(results.join(" | "))
        })()
        return
      }
    }

    // List view shortcuts
    if (view.screen === "list") {
      const entries = getEntries(view.module)
      if (key.name === "j" || key.name === "down") {
        setSelectedIndex((i) => Math.min(entries.length - 1, i + 1))
        return
      }
      if (key.name === "k" || key.name === "up") {
        setSelectedIndex((i) => Math.max(0, i - 1))
        return
      }
      if (key.name === "return" || key.name === "enter") {
        const entry = entries[selectedIndex]
        if (entry) {
          setView({ screen: "detail", module: view.module, key: entry[0] })
        }
        return
      }
      // Pull this module with 'p'
      if (key.name === "p") {
        doPull(view.module)
        return
      }
    }
  })

  const locCount = Object.keys(state.locations).length
  const sessCount = Object.keys(state.sessions).length
  const cdrCount = Object.keys(state.cdrs).length
  const tariffCount = Object.keys(state.tariffs).length
  const tokenCount = Object.keys(state.tokens).length
  const registered = !!state.cpoCredentials
  const counts: Record<OcpiModule, number> = {
    locations: locCount,
    sessions: sessCount,
    cdrs: cdrCount,
    tariffs: tariffCount,
    tokens: tokenCount,
  }

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Header */}
      <box
        border
        borderStyle="single"
        borderColor="#7aa2f7"
        paddingX={2}
        flexDirection="row"
        justifyContent="space-between"
      >
        <text>
          <strong>Mock MSP OCPI 2.1.1</strong>
          {view.screen !== "dashboard" ? (
            <span fg="#565f89">
              {" "}
              {view.screen === "list"
                ? ` > ${view.module}`
                : ` > ${view.module} > ${view.key}`}
            </span>
          ) : null}
        </text>
        <text fg="#565f89">
          {url} | :{port}
        </text>
      </box>

      {/* Main content */}
      <box flexDirection="row" flexGrow={1}>
        {/* Left panel */}
        <box flexDirection="column" width={38}>
          {/* Status */}
          <box
            border
            borderStyle="rounded"
            borderColor="#9ece6a"
            title="Status"
            titleAlignment="left"
            paddingX={1}
            flexDirection="column"
          >
            <box flexDirection="row" justifyContent="space-between">
              <text fg="#565f89">CPO Registered:</text>
              <text fg={registered ? "#9ece6a" : "#f7768e"}>
                {registered
                  ? state.cpoCredentials?.business_details?.name ?? "Yes"
                  : "No"}
              </text>
            </box>
            {MODULES.map((mod) => (
              <box key={mod.id} flexDirection="row" justifyContent="space-between">
                <text fg="#565f89">{mod.label}:</text>
                <text>{counts[mod.id]}</text>
              </box>
            ))}
          </box>

          {view.screen === "dashboard" ? (
            <>
              {/* Module browser */}
              <box
                border
                borderStyle="rounded"
                borderColor="#7dcfff"
                title="Browse"
                titleAlignment="left"
                paddingX={1}
                flexDirection="column"
              >
                {MODULES.map((mod, i) => (
                  <text
                    key={mod.id}
                    fg={i === selectedIndex ? "#7dcfff" : "#c0caf5"}
                  >
                    {i === selectedIndex ? "> " : "  "}
                    {mod.label} ({counts[mod.id]})
                  </text>
                ))}
                <text fg="#565f89">
                  <br />
                  j/k navigate, Enter to open
                </text>
              </box>

              {/* Pull actions */}
              <box
                border
                borderStyle="rounded"
                borderColor="#bb9af7"
                title="Pull"
                titleAlignment="left"
                paddingX={1}
                flexDirection="column"
              >
                {MODULES.map((mod) => (
                  <text key={mod.id}>
                    <span fg="#bb9af7">[{mod.pullKey}]</span> {mod.label}
                  </text>
                ))}
                <text>
                  <span fg="#bb9af7">[a]</span> Pull All
                </text>
                <text>
                  <span fg="#bb9af7">[c]</span> Clear Logs
                </text>
                <text>
                  <span fg="#bb9af7">[q]</span> Quit
                </text>
              </box>
            </>
          ) : (
            /* Navigation help for non-dashboard views */
            <box
              border
              borderStyle="rounded"
              borderColor="#bb9af7"
              title="Navigation"
              titleAlignment="left"
              paddingX={1}
              flexDirection="column"
            >
              <text>
                <span fg="#bb9af7">[j/k]</span> Navigate
              </text>
              <text>
                <span fg="#bb9af7">[Enter]</span>{" "}
                {view.screen === "list" ? "View detail" : ""}
              </text>
              {view.screen === "list" ? (
                <text>
                  <span fg="#bb9af7">[p]</span> Pull {view.module}
                </text>
              ) : null}
              <text>
                <span fg="#bb9af7">[Esc]</span> Back
              </text>
              <text>
                <span fg="#bb9af7">[q]</span> Dashboard
              </text>
            </box>
          )}

          {/* Status message */}
          {statusMsg ? (
            <box paddingX={1} marginTop={1}>
              <text fg="#e0af68">{statusMsg}</text>
            </box>
          ) : null}
        </box>

        {/* Right panel */}
        <box
          flexGrow={1}
          border
          borderStyle="rounded"
          borderColor="#565f89"
          title={
            view.screen === "dashboard"
              ? "Request Log"
              : view.screen === "list"
                ? `${MODULES.find((m) => m.id === view.module)?.label ?? view.module}`
                : view.key
          }
          titleAlignment="left"
          flexDirection="column"
        >
          {view.screen === "dashboard" ? (
            <RequestLog logs={logs} />
          ) : view.screen === "list" ? (
            <ObjectList
              entries={getEntries(view.module)}
              selectedIndex={selectedIndex}
            />
          ) : (
            <ObjectDetail obj={state[view.module][view.key]} />
          )}
        </box>
      </box>
    </box>
  )
}

function RequestLog({ logs }: { logs: LogEntry[] }) {
  return (
    <scrollbox flexGrow={1} focused>
      {logs.length === 0 ? (
        <text fg="#565f89">Waiting for requests...</text>
      ) : (
        [...logs]
          .reverse()
          .slice(0, MAX_LOGS)
          .map((log, i) => {
            const methodColor =
              log.method === "GET"
                ? "#9ece6a"
                : log.method === "POST"
                  ? "#7aa2f7"
                  : log.method === "PUT"
                    ? "#e0af68"
                    : log.method === "PATCH"
                      ? "#bb9af7"
                      : log.method === "DELETE"
                        ? "#f7768e"
                        : log.method === "OUT"
                          ? "#7dcfff"
                          : "#c0caf5"
            return (
              <box key={`${log.timestamp}-${i}`} flexDirection="row" gap={1}>
                <text fg="#565f89">{log.timestamp.substring(11, 19)}</text>
                <text fg={methodColor} width={6}>
                  {log.method}
                </text>
                <text fg="#c0caf5">{log.url}</text>
              </box>
            )
          })
      )}
    </scrollbox>
  )
}

function ObjectList({
  entries,
  selectedIndex,
}: {
  entries: [string, any][]
  selectedIndex: number
}) {
  if (entries.length === 0) {
    return <text fg="#565f89">No objects. Press [p] to pull or [Esc] to go back.</text>
  }

  return (
    <scrollbox flexGrow={1}>
      {entries.map(([key, obj], i) => {
        const isSelected = i === selectedIndex
        const label = obj.name ?? obj.uid ?? obj.id ?? key
        return (
          <box key={key} flexDirection="row" gap={1}>
            <text fg={isSelected ? "#7dcfff" : "#565f89"}>
              {isSelected ? ">" : " "}
            </text>
            <text fg={isSelected ? "#7dcfff" : "#c0caf5"}>{label}</text>
            <text fg="#565f89">{key !== label ? `(${key})` : ""}</text>
          </box>
        )
      })}
    </scrollbox>
  )
}

function ObjectDetail({ obj }: { obj: any }) {
  if (!obj) {
    return <text fg="#f7768e">Object not found</text>
  }

  const json = JSON.stringify(obj, null, 2)
  const lines = json.split("\n")

  return (
    <scrollbox flexGrow={1} focused>
      {lines.map((line, i) => {
        const keyMatch = line.match(/^(\s*)"([^"]+)"(:)\s*(.*)$/)
        if (keyMatch) {
          const [, indent, jsonKey, colon, rest] = keyMatch
          return (
            <text key={i}>
              {indent}<span fg="#7aa2f7">"{jsonKey}"</span>{colon} <span fg={getValueColor(rest)}>{rest}</span>
            </text>
          )
        }
        return (
          <text key={i} fg="#c0caf5">
            {line}
          </text>
        )
      })}
    </scrollbox>
  )
}

function getValueColor(raw: string): string {
  const trimmed = raw.trim().replace(/,$/, "")
  if (trimmed === "true" || trimmed === "false") return "#ff9e64"
  if (trimmed === "null") return "#565f89"
  if (/^-?\d+(\.\d+)?$/.test(trimmed)) return "#ff9e64"
  if (trimmed.startsWith('"') && trimmed.endsWith('"')) return "#9ece6a"
  return "#c0caf5"
}
