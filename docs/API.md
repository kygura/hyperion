# API Reference

Hyperion exposes two interfaces: **HTTP+WebSocket** (daemon API) and **MCP** (LLM tool protocol).

---

## HTTP API (`:8787`)

### Health Check

```
GET /health
```

Returns `200 OK` if the daemon is running.

**Response:**
```json
{
  "status": "ok",
  "uptime_seconds": 3600,
  "reasoning_cycle": 12
}
```

---

### Market Data

#### Get Current Markets

```
GET /markets
```

Returns list of subscribed markets with live orderbooks, funding, and recent trades.

**Query params:**
- `instrument` (optional): filter to one instrument (e.g., `ETH-USD-PERP`)

**Response:**
```json
{
  "markets": [
    {
      "instrument": "ETH-USD-PERP",
      "mid_price": 3205.50,
      "mark_price": 3205.75,
      "funding_rate": 0.00082,
      "funding_period_ms": 3600000,
      "orderbook": {
        "bids": [
          {"price": 3205.00, "size": 10.5},
          {"price": 3204.00, "size": 25.0}
        ],
        "asks": [
          {"price": 3206.00, "size": 12.0},
          {"price": 3207.00, "size": 30.5}
        ]
      },
      "recent_trades": [
        {"price": 3205.25, "side": "BUY", "size": 5.0, "timestamp": "2026-07-09T14:23:45Z"}
      ]
    }
  ]
}
```

---

### Positions

#### Get Current Position

```
GET /positions
```

Returns account holdings, P&L, and exposure.

**Response:**
```json
{
  "account": {
    "collateral": 100000,
    "free_collateral": 45000,
    "leverage": 1.22
  },
  "positions": [
    {
      "instrument": "ETH-USD-PERP",
      "size": 10.5,
      "entry_price": 3195.00,
      "mark_price": 3205.75,
      "unrealized_pnl": 563.375,
      "realized_pnl": 1200.00,
      "funding_accrued": 28.45,
      "liquidation_price": 2900.00
    },
    {
      "instrument": "BTC-USD-PERP",
      "size": 0.5,
      "entry_price": 64200.00,
      "mark_price": 64500.00,
      "unrealized_pnl": 150.00,
      "realized_pnl": 0,
      "funding_accrued": 12.30,
      "liquidation_price": 50000.00
    }
  ],
  "mandate": {
    "goal": "Reach 60/40 split between ETH and USDC",
    "horizon_days": 90,
    "max_drawdown_pct": 8.0,
    "leverage_cap": 2.0
  },
  "mandate_performance": {
    "days_elapsed": 12,
    "pnl_realized": 1200.00,
    "pnl_unrealized": 713.375,
    "max_drawdown_seen": 3.2,
    "current_drawdown": 1.8
  }
}
```

---

### Orders

#### Get Open Orders

```
GET /orders
```

Returns all open orders.

**Response:**
```json
{
  "orders": [
    {
      "order_id": "order-abc123",
      "instrument": "ETH-USD-PERP",
      "side": "BUY",
      "size": 5.0,
      "price": 3200.00,
      "order_type": "LIMIT",
      "status": "OPEN",
      "created_at": "2026-07-09T14:15:00Z",
      "fills": 0
    }
  ]
}
```

#### Place Order

```
POST /orders
```

Create a new order. Goes through risk gates before wire.

**Request body:**
```json
{
  "instrument": "ETH-USD-PERP",
  "side": "BUY",
  "size": 5.0,
  "price": 3200.00,
  "order_type": "LIMIT"
}
```

Optional fields:
- `reduce_only` (boolean): only reduce position size
- `post_only` (boolean): only add to orderbook, never cross

**Response (success):**
```json
{
  "status": "ACCEPTED",
  "order_id": "order-def456",
  "message": "Order placed successfully"
}
```

**Response (gate rejection):**
```json
{
  "status": "REJECTED",
  "order_id": null,
  "message": "Position limit exceeded: would be 35 ETH, max allowed 25",
  "gate_name": "position_limit"
}
```

#### Cancel Order

```
DELETE /orders/{order_id}
```

Cancel an open order.

**Response:**
```json
{
  "status": "CANCELLED",
  "order_id": "order-abc123"
}
```

---

### Decision Journal

#### Get Journal (Paginated)

```
GET /journal
```

Returns append-only journal of all reasoning candidates, decisions, and fills.

**Query params:**
- `limit` (default 100): number of entries to return
- `offset` (default 0): pagination offset
- `instrument` (optional): filter by instrument
- `status` (optional): filter by status (PROPOSED, ACCEPTED, REJECTED, FILLED, CANCELLED)

**Response:**
```json
{
  "entries": [
    {
      "timestamp": "2026-07-09T14:23:45Z",
      "event_type": "candidate",
      "reasoning_cycle": 42,
      "market_picture": "Funding is high at 0.08%; break above 3200 in ETH looks real...",
      "candidate": {
        "instrument": "ETH-USD-PERP",
        "side": "BUY",
        "size": 5.0,
        "price": 3200.00,
        "order_type": "LIMIT"
      },
      "confidence": 8,
      "candor": "Real conviction; regime fits our mandate. Vol is reasonable.",
      "reasoning_model": "claude-3-5-sonnet",
      "status": "PROPOSED"
    },
    {
      "timestamp": "2026-07-09T14:24:12Z",
      "event_type": "decision",
      "order_id": "order-abc123",
      "action": "ACCEPTED",
      "reason": "Operator confirmed in TUI",
      "status": "ACCEPTED"
    },
    {
      "timestamp": "2026-07-09T14:25:30Z",
      "event_type": "fill",
      "order_id": "order-abc123",
      "instrument": "ETH-USD-PERP",
      "side": "BUY",
      "filled_size": 5.0,
      "filled_price": 3198.50,
      "remaining_size": 0,
      "status": "FILLED"
    }
  ],
  "total_count": 1543,
  "offset": 0,
  "limit": 100
}
```

---

### Mandate

#### Get Current Mandate

```
GET /mandate
```

Returns the current trading mandate.

**Response:**
```json
{
  "goal": "Reach 60/40 split between ETH and USDC",
  "horizon_days": 90,
  "max_drawdown_pct": 8.0,
  "leverage_cap": 2.0,
  "created_at": "2026-07-01T10:00:00Z"
}
```

#### Update Mandate

```
PUT /mandate
```

Change the mandate (restarts reasoning with new constraints).

**Request body:**
```json
{
  "goal": "Reach 70/30 split between ETH and USDC",
  "horizon_days": 60,
  "max_drawdown_pct": 5.0,
  "leverage_cap": 1.5
}
```

**Response:**
```json
{
  "status": "UPDATED",
  "mandate": {
    "goal": "Reach 70/30 split between ETH and USDC",
    "horizon_days": 60,
    "max_drawdown_pct": 5.0,
    "leverage_cap": 1.5,
    "created_at": "2026-07-09T15:00:00Z"
  }
}
```

---

### Control

#### Halt (Cancel All, Stop Reasoning)

```
POST /halt
```

Stop all reasoning and cancel all open orders immediately.

**Response:**
```json
{
  "status": "HALTED",
  "open_orders_cancelled": 3,
  "reasoning_stopped": true
}
```

#### Get Mode

```
GET /mode
```

Check whether daemon is in propose or autonomous mode.

**Response:**
```json
{
  "mode": "propose",
  "accepting_manual_orders": true,
  "auto_executing": false
}
```

#### Set Mode

```
PUT /mode
```

Switch between propose and autonomous.

**Request body:**
```json
{
  "mode": "autonomous"
}
```

**Response:**
```json
{
  "mode": "autonomous",
  "auto_executing": true
}
```

---

## WebSocket API

The TUI and dashboard connect via WebSocket for live updates.

### Connect

```
WS ws://localhost:8787/events
```

### Event Stream

After connecting, the server streams JSON events:

```json
{
  "type": "market_update",
  "data": {
    "instrument": "ETH-USD-PERP",
    "mark_price": 3205.75,
    "funding_rate": 0.00082
  }
}
```

```json
{
  "type": "position_update",
  "data": {
    "instrument": "ETH-USD-PERP",
    "size": 10.5,
    "unrealized_pnl": 563.375
  }
}
```

```json
{
  "type": "candidate",
  "data": {
    "reasoning_cycle": 42,
    "candidate": {
      "instrument": "ETH-USD-PERP",
      "side": "BUY",
      "size": 5.0,
      "price": 3200.00
    },
    "confidence": 8,
    "market_picture": "..."
  }
}
```

```json
{
  "type": "order_placed",
  "data": {
    "order_id": "order-abc123",
    "instrument": "ETH-USD-PERP",
    "side": "BUY",
    "size": 5.0,
    "price": 3200.00
  }
}
```

```json
{
  "type": "order_filled",
  "data": {
    "order_id": "order-abc123",
    "filled_size": 5.0,
    "filled_price": 3198.50
  }
}
```

---

## MCP Protocol

Hyperion exposes a Model Context Protocol (MCP) server for LLM clients like Claude.

### Register

```bash
claude mcp add hyperion -- ./backend/hyperagent mcp -address 0x...
```

### Tools

#### `read_markets`

Get live orderbook and funding data for one or more instruments.

**Input:**
```json
{
  "instruments": ["ETH-USD-PERP", "BTC-USD-PERP"]
}
```

**Output:**
```json
{
  "markets": [
    {
      "instrument": "ETH-USD-PERP",
      "mid_price": 3205.50,
      "mark_price": 3205.75,
      "funding_rate": 0.00082,
      "orderbook": {...},
      "recent_trades": [...]
    }
  ]
}
```

#### `read_positions`

Get current holdings and P&L.

**Input:** (no params)

**Output:**
```json
{
  "account": {
    "collateral": 100000,
    "free_collateral": 45000,
    "leverage": 1.22
  },
  "positions": [...],
  "mandate": {...}
}
```

#### `place_order`

Place a new order (goes through risk gates).

**Input:**
```json
{
  "instrument": "ETH-USD-PERP",
  "side": "BUY",
  "size": 5.0,
  "price": 3200.00,
  "order_type": "LIMIT"
}
```

**Output:**
```json
{
  "status": "ACCEPTED",
  "order_id": "order-def456",
  "message": "Order placed successfully"
}
```

or (on rejection):

```json
{
  "status": "REJECTED",
  "message": "Position limit exceeded",
  "gate_name": "position_limit"
}
```

#### `cancel_order`

Cancel an open order.

**Input:**
```json
{
  "order_id": "order-abc123"
}
```

**Output:**
```json
{
  "status": "CANCELLED",
  "order_id": "order-abc123"
}
```

---

## Error Handling

### HTTP Status Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 400 | Bad request (malformed input) |
| 401 | Unauthorized (if auth is enabled) |
| 404 | Resource not found |
| 500 | Server error |

### Error Response

```json
{
  "error": "Position limit exceeded",
  "code": "GATE_REJECTION",
  "details": {
    "gate": "position_limit",
    "limit": 25,
    "would_be": 35
  }
}
```

---

## Rate Limiting

No built-in rate limits in the daemon; assumes trusted clients (TUI, dashboard, MCP).

For public deployments, add a reverse proxy (Cloudflare, nginx) to rate-limit.

---

## Authentication

Currently, no auth (assumes localhost or private network).

For production, add:
- API key validation (HTTP header or query param)
- TLS/HTTPS
- WebSocket auth token
