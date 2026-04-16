const express = require('express');
const app = express();
const port = process.env.PORT || 3010;

app.use(express.json());

// OCPI response envelope
function ocpiResponse(data, statusCode = 1000, statusMessage = 'Success') {
    return {
        data,
        status_code: statusCode,
        status_message: statusMessage,
        timestamp: new Date().toISOString()
    };
}

function requestLogger(req, res, next) {
    console.log(`[${new Date().toISOString()}] ${req.method} ${req.url}`);
    if (req.body && Object.keys(req.body).length > 0) {
        console.log('Body:', JSON.stringify(req.body));
    }
    next();
}

const url = `https://e489-77-163-42-32.ngrok-free.app`

app.use(requestLogger);

// ---- VERSIONS ----

app.get('/ocpi/versions', (req, res) => {
    res.json(ocpiResponse([
        { version: '2.1.1', url: `${url}/ocpi/2.1.1` }
    ]));
});

app.get('/ocpi/2.1.1', (req, res) => {
    res.json(ocpiResponse({
        version: '2.1.1',
        endpoints: [
            { identifier: 'credentials', url: `${url}/ocpi/2.1.1/credentials` },
            { identifier: 'locations',   url: `${url}/ocpi/receiver/2.1.1/locations` },
            { identifier: 'sessions',    url: `${url}/ocpi/receiver/2.1.1/sessions` },
            { identifier: 'cdrs',        url: `${url}/ocpi/receiver/2.1.1/cdrs` },
            { identifier: 'tariffs',     url: `${url}/ocpi/receiver/2.1.1/tariffs` },
            { identifier: 'tokens',      url: `${url}/ocpi/sender/2.1.1/tokens` },
            { identifier: 'commands',    url: `${url}/ocpi/receiver/2.1.1/commands` },
        ]
    }));
});

// ---- CREDENTIALS ----

const MSP_CREDENTIALS = {
    token: 'mocked-msp-token',
    url: `${url}/ocpi/versions`,
    business_details: { name: 'Mock MFC' },
    party_id: 'MFC',
    country_code: 'NL'
};

app.get('/ocpi/2.1.1/credentials', (req, res) => {
    res.json(ocpiResponse(MSP_CREDENTIALS));
});

// Store credentials received from CPO after handshake
let cpoCredentials = null;

app.put('/ocpi/2.1.1/credentials', async (req, res) => {
    console.log("Credentials call")
    if (!req.body.token) {
        return res.status(400).json(ocpiResponse(null, 2001, 'Token is required'));
    }
    console.log('Credentials registered from:', req.body.business_details?.name ?? 'unknown');

    // Save CPO credentials
    cpoCredentials = req.body;
    console.log('Saved CPO credentials:', JSON.stringify(cpoCredentials, null, 2));

    res.json(ocpiResponse(MSP_CREDENTIALS));

    // After successful handshake, pull locations from the CPO
    try {
        await pullCpoLocations();
    } catch (err) {
        console.error('Failed to pull CPO locations:', err.message);
    }
});

async function pullCpoLocations() {
    if (!cpoCredentials?.token || !cpoCredentials?.url) {
        console.error('No CPO credentials available to pull locations');
        return;
    }

    const headers = { Authorization: `Token ${cpoCredentials.token}` };

    // 1. Fetch versions from the CPO
    console.log(`Fetching CPO versions from ${cpoCredentials.url}`);
    const versionsRes = await fetch(cpoCredentials.url, { headers });
    const versionsBody = await versionsRes.json();
    const version = versionsBody.data?.find(v => v.version === '2.1.1');
    if (!version) {
        console.error('CPO does not support OCPI 2.1.1');
        return;
    }

    // 2. Fetch version details to discover endpoints
    console.log(`Fetching CPO version details from ${version.url}`);
    const detailsRes = await fetch(version.url, { headers });
    const detailsBody = await detailsRes.json();
    const locationsEndpoint = detailsBody.data?.endpoints?.find(e => e.identifier === 'locations');
    if (!locationsEndpoint) {
        console.error('CPO has no locations endpoint');
        return;
    }

    // 3. Pull all locations
    console.log(`Pulling CPO locations from ${locationsEndpoint.url}`);
    const locationsRes = await fetch(locationsEndpoint.url, { headers });
    const locationsBody = await locationsRes.json();

    console.log(`Received ${Array.isArray(locationsBody.data) ? locationsBody.data.length : 0} locations from CPO:`);
    if (Array.isArray(locationsBody.data)) {
        locationsBody.data.forEach(loc => {
            console.log('Location:', JSON.stringify(loc, null, 2));
        });
    }
}

app.put('/ocpi/2.1.1/credentials', (req, res) => {
    if (!req.body.token) {
        return res.status(400).json(ocpiResponse(null, 2001, 'Token is required'));
    }
    res.json(ocpiResponse(MSP_CREDENTIALS));
});

app.delete('/ocpi/2.1.1/credentials', (req, res) => {
    res.json(ocpiResponse(null));
});

// ---- TOKENS (MSP is SENDER — CPO pulls / requests authorization) ----

const tokens = {
    'valid-token-1': {
        uid: 'valid-token-1',
        type: 'RFID',
        auth_id: 'NL-MFC-valid-token-1',
        visual_number: 'NL-MFC-000001',
        issuer: 'Mock MSP',
        valid: true,
        whitelist: 'ALLOWED',
        last_updated: '2025-01-01T00:00:00Z'
    },
    'valid-token-2': {
        uid: 'valid-token-2',
        type: 'RFID',
        auth_id: 'NL-MFC-valid-token-2',
        visual_number: 'NL-MFC-000002',
        issuer: 'Mock MFC',
        valid: true,
        whitelist: 'ALLOWED',
        last_updated: '2025-01-01T00:00:00Z'
    }
};

// CPO pulls full token list
app.get('/ocpi/sender/2.1.1/tokens', (req, res) => {
    res.json(ocpiResponse(Object.values(tokens)));
});

// CPO pulls a single token
app.get('/ocpi/sender/2.1.1/tokens/:countryCode/:partyId/:tokenUid', (req, res) => {
    const token = tokens[req.params.tokenUid];
    if (!token) {
        return res.status(404).json(ocpiResponse(null, 2004, 'Token not found'));
    }
    res.json(ocpiResponse(token));
});

// CPO requests real-time authorization
app.post('/ocpi/sender/2.1.1/tokens/:tokenUid/authorize', (req, res) => {
    const token = tokens[req.params.tokenUid];

    if (!token) {
        return res.status(404).json(ocpiResponse(null, 2004, 'Unknown token'));
    }

    // Simulate authorization processing delay
    setTimeout(() => {
        res.json(ocpiResponse({ allowed: token.valid ? 'ALLOWED' : 'NOT_ALLOWED' }));
    }, 10000);
});

// ---- LOCATIONS (MSP is RECEIVER — CPO pushes) ----

const locations = {};

app.put('/ocpi/receiver/2.1.1/locations/:countryCode/:partyId/:locationId', (req, res) => {
    const { countryCode, partyId, locationId } = req.params;
    const key = `${countryCode}/${partyId}/${locationId}`;
    locations[key] = { ...req.body, country_code: countryCode, party_id: partyId, id: locationId };
    console.log(`Location upserted: ${key}`);
    res.json(ocpiResponse(null));
});

app.patch('/ocpi/receiver/2.1.1/locations/:countryCode/:partyId/:locationId', (req, res) => {
    const { countryCode, partyId, locationId } = req.params;
    const key = `${countryCode}/${partyId}/${locationId}`;
    locations[key] = { ...(locations[key] ?? {}), ...req.body };
    console.log(`Location patched: ${key}`);
    res.json(ocpiResponse(null));
});

// ---- SESSIONS (MSP is RECEIVER — CPO pushes) ----

const sessions = {};

app.put('/ocpi/receiver/2.1.1/sessions/:countryCode/:partyId/:sessionId', (req, res) => {
    const { countryCode, partyId, sessionId } = req.params;
    const key = `${countryCode}/${partyId}/${sessionId}`;
    sessions[key] = { ...req.body, country_code: countryCode, party_id: partyId, id: sessionId };
    console.log(`Session upserted: ${key}`);
    res.json(ocpiResponse(null));
});

app.patch('/ocpi/receiver/2.1.1/sessions/:countryCode/:partyId/:sessionId', (req, res) => {
    const { countryCode, partyId, sessionId } = req.params;
    const key = `${countryCode}/${partyId}/${sessionId}`;
    sessions[key] = { ...(sessions[key] ?? {}), ...req.body };
    console.log(`Session patched: ${key}`);
    res.json(ocpiResponse(null));
});

// ---- CDRs (MSP is RECEIVER — CPO posts) ----

const cdrs = {};

app.post('/ocpi/receiver/2.1.1/cdrs', (req, res) => {
    const cdr = req.body;
    cdrs[cdr.id] = cdr;
    console.log(`CDR received: ${cdr.id}`);
    res.set('Location', `${url}/ocpi/receiver/2.1.1/cdrs/${cdr.id}`);
    res.status(201).json(ocpiResponse(null));
});

app.get('/ocpi/receiver/2.1.1/cdrs/:cdrId', (req, res) => {
    const cdr = cdrs[req.params.cdrId];
    if (!cdr) {
        return res.status(404).json(ocpiResponse(null, 2004, 'CDR not found'));
    }
    res.json(ocpiResponse(cdr));
});

// ---- TARIFFS (MSP is RECEIVER — CPO pushes) ----

const tariffs = {};

app.put('/ocpi/receiver/2.1.1/tariffs/:countryCode/:partyId/:tariffId', (req, res) => {
    const { countryCode, partyId, tariffId } = req.params;
    const key = `${countryCode}/${partyId}/${tariffId}`;
    tariffs[key] = { ...req.body, country_code: countryCode, party_id: partyId, id: tariffId };
    console.log(`Tariff upserted: ${key}`);
    res.json(ocpiResponse(null));
});

app.delete('/ocpi/receiver/2.1.1/tariffs/:countryCode/:partyId/:tariffId', (req, res) => {
    const { countryCode, partyId, tariffId } = req.params;
    const key = `${countryCode}/${partyId}/${tariffId}`;
    delete tariffs[key];
    console.log(`Tariff deleted: ${key}`);
    res.json(ocpiResponse(null));
});

// ---- COMMANDS (MSP is RECEIVER for async command results from CPO) ----

app.post('/ocpi/receiver/2.1.1/commands/:command/:uid', (req, res) => {
    const { command, uid } = req.params;
    console.log(`Command result received: ${command} uid=${uid}`, req.body);
    res.json(ocpiResponse(null));
});

// ---- Catch-all ----

app.all('*', (req, res) => {
    console.log(`Unhandled: ${req.method} ${req.url}`);
    res.status(404).json(ocpiResponse(null, 2000, `Endpoint not found: ${req.url}`));
});

app.listen(port, () => {
    console.log(`Mock MSP OCPI 2.1.1 server running on http://localhost:${port}`);
});
