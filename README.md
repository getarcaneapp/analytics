# Arcane Analytics

Modified Analytics server originally from [Pocket ID](https://github.com/pocket-id/pocket-id)

A lightweight analytics service that collects heartbeat data from Arcaneinstances to count active deployments.

## Overview

Seeing how many active Arcaneinstances are out there through our analytics server genuinely motivates our team to keep developing and maintaining the project. The instance count is also displayed on the [Arcane Website](https://getarcane.app).

## Data Collection

Only minimal, non-identifiable data is collected, and analytics can be completely disabled by users.

The server stores only the following information:

| Field            | Description                                                |
| :--------------- | :--------------------------------------------------------- |
| **Instance ID**  | A unique, non-identifiable UUID for the Arcaneinstance |
| **First seen**   | Timestamp when the instance first sent a heartbeat         |
| **Last seen**    | Timestamp of the most recent heartbeat                     |
| **Last version** | Version of the Arcaneinstance                          |
| **Server type**  | Instance role (`manager` or `agent`)                       |

### Activity Status

- **Active**: Instance has sent a heartbeat within the last 2 days
- **Inactive**: No heartbeat received for 2+ consecutive days

## API Endpoints

This server is hosted at `https://checkin.getarcane.app`.

### Get Statistics

```http
GET /stats
```

Returns active instance count, inactive count, and historical data.

**Query Parameters:**

- `timeframe` (optional): Data timeframe
  - `daily` - Daily counts for last 30 days (default)
  - `monthly` - Monthly counts

**Example Response:**

```json
{
  "total": 5,
  "inactive": 2,
  "by_type": {
    "manager": 1,
    "agent": 3,
    "unknown": 1
  },
  "by_version": {
    "1.0.0": 2,
    "1.1.0": 3
  },
  "history": [
    {
      "date": "2025-05-23",
      "count": 1
    },
    {
      "date": "2025-05-24",
      "count": 3
    },
    {
      "date": "2025-05-25",
      "count": 5
    }
  ]
}
```

### Send Heartbeat

```http
POST /heartbeat
```

Registers or updates an instance's heartbeat.

**Request Body:**

```json
{
  "instance_id": "b316815f-5f81-488f-89f8-12b62013dfa4",
  "version": "1.0.0",
  "server_type": "agent"
}
```

**Parameters:**

- `instance_id` (string, required): Unique UUID for the Arcaneinstance
- `version` (string, required): Current version of the Arcaneinstance
- `server_type` (string, optional): `manager` or `agent` (omit if unknown)

---
