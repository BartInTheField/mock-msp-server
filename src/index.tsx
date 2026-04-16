import { createCliRenderer } from "@opentui/core"
import { createRoot } from "@opentui/react"
import { createServer, type LogEntry } from "./server"
import { App } from "./App"
const args = Bun.argv.slice(2)

function getArg(name: string, short: string, defaultValue: string): string {
  for (let i = 0; i < args.length; i++) {
    if (args[i] === `--${name}` || args[i] === `-${short}`) {
      return args[i + 1] ?? defaultValue
    }
    if (args[i]?.startsWith(`--${name}=`)) {
      return args[i].split("=")[1] ?? defaultValue
    }
  }
  return defaultValue
}

const values = {
  url: getArg("url", "u", "http://localhost:3010"),
  port: getArg("port", "p", "3010"),
}

const url = values.url!
const port = parseInt(values.port!, 10)

const logs: LogEntry[] = []
let triggerRender: () => void = () => {}

let renderScheduled = false
function scheduleRender() {
  if (!renderScheduled) {
    renderScheduled = true
    queueMicrotask(() => {
      renderScheduled = false
      triggerRender()
    })
  }
}

const onLog = (entry: LogEntry) => {
  logs.push(entry)
  if (logs.length > 200) logs.splice(0, logs.length - 200)
  scheduleRender()
}

const { state } = createServer({
  url,
  port,
  onLog,
  onStateChange: () => scheduleRender(),
})

const renderer = await createCliRenderer({
  exitOnCtrlC: false,
})

function renderApp() {
  root.render(
    <App url={url} port={port} state={state} logs={logs} onLog={onLog} onStateChange={scheduleRender} />
  )
}

triggerRender = renderApp

const root = createRoot(renderer)
renderApp()
