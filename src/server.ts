import express from "express"

export interface ServerState {
  cpoCredentials: any | null
  locations: Record<string, any>
  sessions: Record<string, any>
  cdrs: Record<string, any>
  tariffs: Record<string, any>
  tokens: Record<string, any>
}

export interface LogEntry {
  timestamp: string
  method: string
  url: string
  body?: string
}

export interface ServerConfig {
  url: string
  port: number
  onLog: (entry: LogEntry) => void
  onStateChange: () => void
}

export function createServer(config: ServerConfig) {
  const { url, port, onLog, onStateChange } = config

  const app = express()
  app.use(express.json())

  const state: ServerState = {
    cpoCredentials: null,
    locations: {},
    sessions: {},
    cdrs: {},
    tariffs: {},
    tokens: {
      "valid-token-1": {
        uid: "valid-token-1",
        type: "RFID",
        auth_id: "NL-MFC-valid-token-1",
        visual_number: "NL-MFC-000001",
        issuer: "Mock MSP",
        valid: true,
        whitelist: "ALLOWED",
        last_updated: "2025-01-01T00:00:00Z",
      },
      "valid-token-2": {
        uid: "valid-token-2",
        type: "RFID",
        auth_id: "NL-MFC-valid-token-2",
        visual_number: "NL-MFC-000002",
        issuer: "Mock MFC",
        valid: true,
        whitelist: "ALLOWED",
        last_updated: "2025-01-01T00:00:00Z",
      },
    },
  }

  function ocpiResponse(
    data: any,
    statusCode = 1000,
    statusMessage = "Success"
  ) {
    return {
      data,
      status_code: statusCode,
      status_message: statusMessage,
      timestamp: new Date().toISOString(),
    }
  }

  const MSP_CREDENTIALS = {
    token: "mocked-msp-token",
    url: `${url}/ocpi/versions`,
    business_details: { name: "Mock MFC" },
    party_id: "MFC",
    country_code: "NL",
  }

  // Request logger middleware
  app.use((req, res, next) => {
    const entry: LogEntry = {
      timestamp: new Date().toISOString(),
      method: req.method,
      url: req.url,
    }
    if (req.body && Object.keys(req.body).length > 0) {
      entry.body = JSON.stringify(req.body)
    }
    onLog(entry)
    next()
  })

  // ---- VERSIONS ----
  app.get("/ocpi/versions", (_req, res) => {
    res.json(ocpiResponse([{ version: "2.1.1", url: `${url}/ocpi/2.1.1` }]))
  })

  app.get("/ocpi/2.1.1", (_req, res) => {
    res.json(
      ocpiResponse({
        version: "2.1.1",
        endpoints: [
          {
            identifier: "credentials",
            url: `${url}/ocpi/2.1.1/credentials`,
          },
          {
            identifier: "locations",
            url: `${url}/ocpi/receiver/2.1.1/locations`,
          },
          {
            identifier: "sessions",
            url: `${url}/ocpi/receiver/2.1.1/sessions`,
          },
          { identifier: "cdrs", url: `${url}/ocpi/receiver/2.1.1/cdrs` },
          {
            identifier: "tariffs",
            url: `${url}/ocpi/receiver/2.1.1/tariffs`,
          },
          {
            identifier: "tokens",
            url: `${url}/ocpi/sender/2.1.1/tokens`,
          },
          {
            identifier: "commands",
            url: `${url}/ocpi/receiver/2.1.1/commands`,
          },
        ],
      })
    )
  })

  // ---- CREDENTIALS ----
  app.get("/ocpi/2.1.1/credentials", (_req, res) => {
    res.json(ocpiResponse(MSP_CREDENTIALS))
  })

  app.put("/ocpi/2.1.1/credentials", async (req, res) => {
    if (!req.body.token) {
      return res.status(400).json(ocpiResponse(null, 2001, "Token is required"))
    }
    state.cpoCredentials = req.body
    onStateChange()
    res.json(ocpiResponse(MSP_CREDENTIALS))
  })

  app.delete("/ocpi/2.1.1/credentials", (_req, res) => {
    res.json(ocpiResponse(null))
  })

  // ---- TOKENS ----
  app.get("/ocpi/sender/2.1.1/tokens", (_req, res) => {
    res.json(ocpiResponse(Object.values(state.tokens)))
  })

  app.get(
    "/ocpi/sender/2.1.1/tokens/:countryCode/:partyId/:tokenUid",
    (req, res) => {
      const token = state.tokens[req.params.tokenUid]
      if (!token) {
        return res
          .status(404)
          .json(ocpiResponse(null, 2004, "Token not found"))
      }
      res.json(ocpiResponse(token))
    }
  )

  app.post("/ocpi/sender/2.1.1/tokens/:tokenUid/authorize", (req, res) => {
    const token = state.tokens[req.params.tokenUid]
    if (!token) {
      return res.status(404).json(ocpiResponse(null, 2004, "Unknown token"))
    }
    setTimeout(() => {
      res.json(
        ocpiResponse({ allowed: token.valid ? "ALLOWED" : "NOT_ALLOWED" })
      )
    }, 10000)
  })

  // ---- LOCATIONS ----
  app.put(
    "/ocpi/receiver/2.1.1/locations/:countryCode/:partyId/:locationId",
    (req, res) => {
      const { countryCode, partyId, locationId } = req.params
      const key = `${countryCode}/${partyId}/${locationId}`
      state.locations[key] = {
        ...req.body,
        country_code: countryCode,
        party_id: partyId,
        id: locationId,
      }
      onStateChange()
      res.json(ocpiResponse(null))
    }
  )

  app.patch(
    "/ocpi/receiver/2.1.1/locations/:countryCode/:partyId/:locationId",
    (req, res) => {
      const { countryCode, partyId, locationId } = req.params
      const key = `${countryCode}/${partyId}/${locationId}`
      state.locations[key] = { ...(state.locations[key] ?? {}), ...req.body }
      onStateChange()
      res.json(ocpiResponse(null))
    }
  )

  // ---- SESSIONS ----
  app.put(
    "/ocpi/receiver/2.1.1/sessions/:countryCode/:partyId/:sessionId",
    (req, res) => {
      const { countryCode, partyId, sessionId } = req.params
      const key = `${countryCode}/${partyId}/${sessionId}`
      state.sessions[key] = {
        ...req.body,
        country_code: countryCode,
        party_id: partyId,
        id: sessionId,
      }
      onStateChange()
      res.json(ocpiResponse(null))
    }
  )

  app.patch(
    "/ocpi/receiver/2.1.1/sessions/:countryCode/:partyId/:sessionId",
    (req, res) => {
      const { countryCode, partyId, sessionId } = req.params
      const key = `${countryCode}/${partyId}/${sessionId}`
      state.sessions[key] = { ...(state.sessions[key] ?? {}), ...req.body }
      onStateChange()
      res.json(ocpiResponse(null))
    }
  )

  // ---- CDRs ----
  app.post("/ocpi/receiver/2.1.1/cdrs", (req, res) => {
    const cdr = req.body
    state.cdrs[cdr.id] = cdr
    onStateChange()
    res.set("Location", `${url}/ocpi/receiver/2.1.1/cdrs/${cdr.id}`)
    res.status(201).json(ocpiResponse(null))
  })

  app.get("/ocpi/receiver/2.1.1/cdrs/:cdrId", (req, res) => {
    const cdr = state.cdrs[req.params.cdrId]
    if (!cdr) {
      return res.status(404).json(ocpiResponse(null, 2004, "CDR not found"))
    }
    res.json(ocpiResponse(cdr))
  })

  // ---- TARIFFS ----
  app.put(
    "/ocpi/receiver/2.1.1/tariffs/:countryCode/:partyId/:tariffId",
    (req, res) => {
      const { countryCode, partyId, tariffId } = req.params
      const key = `${countryCode}/${partyId}/${tariffId}`
      state.tariffs[key] = {
        ...req.body,
        country_code: countryCode,
        party_id: partyId,
        id: tariffId,
      }
      onStateChange()
      res.json(ocpiResponse(null))
    }
  )

  app.delete(
    "/ocpi/receiver/2.1.1/tariffs/:countryCode/:partyId/:tariffId",
    (req, res) => {
      const { countryCode, partyId, tariffId } = req.params
      const key = `${countryCode}/${partyId}/${tariffId}`
      delete state.tariffs[key]
      onStateChange()
      res.json(ocpiResponse(null))
    }
  )

  // ---- COMMANDS ----
  app.post("/ocpi/receiver/2.1.1/commands/:command/:uid", (req, res) => {
    res.json(ocpiResponse(null))
  })

  // ---- Catch-all ----
  app.all("*", (req, res) => {
    res
      .status(404)
      .json(ocpiResponse(null, 2000, `Endpoint not found: ${req.url}`))
  })

  const server = app.listen(port)

  return { state, server }
}

export type OcpiModule = "locations" | "sessions" | "cdrs" | "tariffs" | "tokens"

const MODULE_ENDPOINT_IDS: Record<OcpiModule, string> = {
  locations: "locations",
  sessions: "sessions",
  cdrs: "cdrs",
  tariffs: "tariffs",
  tokens: "tokens",
}

async function discoverEndpoint(
  state: ServerState,
  module: OcpiModule,
  onLog: (entry: LogEntry) => void
): Promise<{ url: string } | string> {
  if (!state.cpoCredentials?.token || !state.cpoCredentials?.url) {
    return "No CPO credentials available. Register first."
  }

  const headers = { Authorization: `Token ${state.cpoCredentials.token}` }

  onLog({ timestamp: new Date().toISOString(), method: "OUT", url: state.cpoCredentials.url })
  const versionsRes = await fetch(state.cpoCredentials.url, { headers })
  const versionsBody = await versionsRes.json()
  const version = (versionsBody as any).data?.find((v: any) => v.version === "2.1.1")
  if (!version) return "CPO does not support OCPI 2.1.1"

  onLog({ timestamp: new Date().toISOString(), method: "OUT", url: version.url })
  const detailsRes = await fetch(version.url, { headers })
  const detailsBody = await detailsRes.json()
  const endpoint = (detailsBody as any).data?.endpoints?.find(
    (e: any) => e.identifier === MODULE_ENDPOINT_IDS[module]
  )
  if (!endpoint) return `CPO has no ${module} endpoint`

  return { url: endpoint.url }
}

function storeItems(state: ServerState, module: OcpiModule, items: any[]) {
  const store = state[module] as Record<string, any>
  for (const item of items) {
    let key: string
    if (module === "cdrs") {
      key = item.id ?? `cdr-${Date.now()}`
    } else if (module === "tokens") {
      key = item.uid ?? item.id ?? `token-${Date.now()}`
    } else {
      key = `${item.country_code}/${item.party_id}/${item.id}`
    }
    store[key] = item
  }
}

export async function pullModule(
  state: ServerState,
  module: OcpiModule,
  onLog: (entry: LogEntry) => void
): Promise<string> {
  try {
    const result = await discoverEndpoint(state, module, onLog)
    if (typeof result === "string") return result

    const headers = { Authorization: `Token ${state.cpoCredentials.token}` }
    onLog({ timestamp: new Date().toISOString(), method: "OUT", url: result.url })
    const res = await fetch(result.url, { headers })
    const body = await res.json()
    const items = (body as any).data

    if (Array.isArray(items)) {
      storeItems(state, module, items)
      return `Pulled ${items.length} ${module} from CPO`
    }
    return `No ${module} in response`
  } catch (err: any) {
    return `Failed: ${err.message}`
  }
}
