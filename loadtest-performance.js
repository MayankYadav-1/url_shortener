import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    redirects: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '30s',
      preAllocatedVUs: 20,
      maxVUs: 100,
    },
  },
};

export default function () {
  const apiKey = `loadtest-vu-${__VU}`;

  const res = http.get('http://localhost/1', {
    redirects: 0,
    headers: {
      'X-API-Key': apiKey,
    },
  });

  check(res, {
    'redirect response': (r) => r.status === 302 || r.status === 404,
  });
}