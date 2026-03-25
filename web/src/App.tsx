import React from 'react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import AppLayout from './components/Layout';
import Accounts from './pages/Accounts';
import AccountDetail from './pages/AccountDetail';
import Dashboard from './pages/Dashboard';
import Groups from './pages/Groups';
import Keys from './pages/Keys';
import Logs from './pages/Logs';
import Settings from './pages/Settings';
import Stats from './pages/Stats';

const App: React.FC = () => (
  <BrowserRouter>
    <Routes>
      <Route element={<AppLayout />}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/accounts" element={<Accounts />} />
        <Route path="/accounts/:id" element={<AccountDetail />} />
        <Route path="/groups" element={<Groups />} />
        <Route path="/keys" element={<Keys />} />
        <Route path="/logs" element={<Logs />} />
        <Route path="/stats" element={<Stats />} />
        <Route path="/settings" element={<Settings />} />
      </Route>
    </Routes>
  </BrowserRouter>
);

export default App;
