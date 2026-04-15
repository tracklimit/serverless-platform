import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE || 'http://localhost:8080';
const WORKSPACE = __ENV.WORKSPACE || 'demo';
const FUNCTION = __ENV.FUNCTION || 'bench';
const URL = `${BASE}/fn/${WORKSPACE}/${FUNCTION}`;

export const options = {
  scenarios: {
    warmup: {
      executor: 'constant-vus',
      vus: 5,
      duration: '30s',
      gracefulStop: '0s',
      tags: { phase: 'warmup' },
    },
    steady: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 50 },
        { duration: '2m', target: 50 },
      ],
      startTime: '30s',
      tags: { phase: 'steady' },
    },
  },
  thresholds: {
    'http_req_failed{phase:steady}': ['rate<0.01'],
    'http_req_duration{phase:steady}': ['p(95)<500', 'p(99)<1000'],
  },
  discardResponseBodies: true,
};

const payload = JSON.stringify({ message: 'bench' });
const params = { headers: { 'Content-Type': 'application/json' } };

export default function () {
  const res = http.post(URL, payload, params);
  check(res, { 'status is 2xx': (r) => r.status >= 200 && r.status < 300 });
}
