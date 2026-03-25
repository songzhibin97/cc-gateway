import axios from 'axios';

const client = axios.create({
  baseURL: import.meta.env.VITE_ADMIN_API_URL || '',
  timeout: 30000,
});

client.interceptors.request.use((config) => {
  const token = localStorage.getItem('admin_token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      window.dispatchEvent(new CustomEvent('auth-error'));
    }
    return Promise.reject(error);
  },
);

export default client;
