import React, { useEffect, useMemo, useState } from 'react';
import type { AxiosError } from 'axios';
import {
  Alert,
  Button,
  Card,
  Col,
  Divider,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Typography,
  message,
} from 'antd';
import type { TableProps } from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import {
  createAccount,
  deleteAccount,
  listAccounts,
  resetBreaker,
  updateAccount,
  updateAccountStatus,
} from '../api/accounts';
import {
  providerAlertMap,
  providerLabelMap,
  providerOptions,
  renderAccountStatus,
  renderBreakerState,
} from '../components/accountUi';
import type { Account } from '../types';

interface AliasFormItem {
  from: string;
  to: string;
}

interface AccountFormValues {
  id: string;
  name: string;
  provider: Account['provider'];
  api_key: string;
  base_url: string;
  proxy_url: string;
  user_agent: string;
  allowed_models: string[];
  model_aliases: AliasFormItem[];
  max_concurrent: number;
  failure_threshold: number;
  success_threshold: number;
  open_duration: string;
  thinking_effort?: string;
}

const getErrorMessage = (error: unknown) => {
  const axiosError = error as AxiosError<{ message?: string; error?: string }>;
  return axiosError.response?.data?.message ?? axiosError.response?.data?.error ?? axiosError.message ?? '请求失败';
};

const getInitialFormValues = (): AccountFormValues => ({
  id: '',
  name: '',
  provider: 'openai',
  api_key: '',
  base_url: '',
  proxy_url: '',
  user_agent: '',
  allowed_models: [],
  model_aliases: [],
  max_concurrent: 0,
  failure_threshold: 5,
  success_threshold: 2,
  open_duration: '60s',
  thinking_effort: '',
});

const toAliasArray = (aliases: Account['model_aliases']) =>
  Object.entries(aliases ?? {}).map(([from, to]) => ({ from, to }));

const unpackAccountToForm = (account: Account): AccountFormValues => {
  const extra = account.extra ?? {};

  return {
    id: account.id,
    name: account.name,
    provider: account.provider,
    api_key: '',
    base_url: account.base_url,
    proxy_url: account.proxy_url,
    user_agent: account.user_agent,
    allowed_models: account.allowed_models && account.allowed_models.length > 0 ? account.allowed_models : [],
    model_aliases: toAliasArray(account.model_aliases),
    max_concurrent: account.max_concurrent ?? 0,
    failure_threshold: account.circuit_breaker?.failure_threshold ?? 5,
    success_threshold: account.circuit_breaker?.success_threshold ?? 2,
    open_duration: account.circuit_breaker?.open_duration ?? '60s',
    thinking_effort: extra.thinking_effort ?? extra.reasoning_effort ?? '',
  };
};

const packExtra = (values: AccountFormValues) => {
  const extra: Record<string, unknown> = {};

  if (values.thinking_effort && values.thinking_effort !== '') {
    extra.thinking_effort = values.thinking_effort;
  }

  return Object.keys(extra).length > 0 ? extra : null;
};

const buildPayload = (values: AccountFormValues): Partial<Account> => {
  const allowedModels = values.allowed_models.map((item) => item.trim()).filter(Boolean);
  const modelAliases = values.model_aliases.reduce<Record<string, string>>((acc, item) => {
    const from = item.from.trim();
    const to = item.to.trim();
    if (from && to) {
      acc[from] = to;
    }
    return acc;
  }, {});

  return {
    id: values.id.trim(),
    name: values.name.trim(),
    provider: values.provider,
    api_key: values.api_key.trim(),
    base_url: values.base_url.trim(),
    proxy_url: values.proxy_url.trim(),
    user_agent: values.user_agent.trim(),
    allowed_models: allowedModels,
    model_aliases: Object.keys(modelAliases).length > 0 ? modelAliases : null,
    max_concurrent: values.max_concurrent,
    circuit_breaker: {
      failure_threshold: values.failure_threshold,
      success_threshold: values.success_threshold,
      open_duration: values.open_duration.trim(),
    },
    extra: packExtra(values),
    status: 'enabled',
  };
};

const diffPayload = (nextPayload: Partial<Account>, prevPayload: Partial<Account>) => {
  const changedEntries = Object.entries(nextPayload).filter(([key, value]) => {
    const previous = prevPayload[key as keyof Account];
    return JSON.stringify(previous) !== JSON.stringify(value);
  });

  return Object.fromEntries(changedEntries) as Partial<Account>;
};

const renderAllowedModels = (models: string[]) => {
  if (models.length === 0) {
    return '-';
  }

  const text = models.join(', ');
  return (
    <Typography.Text ellipsis={{ tooltip: text }} style={{ maxWidth: 240 }}>
      {text}
    </Typography.Text>
  );
};

const Accounts: React.FC = () => {
  const [form] = Form.useForm<AccountFormValues>();
  const [messageApi, contextHolder] = message.useMessage();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState<Account | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [searchText, setSearchText] = useState('');
  const [providerFilter, setProviderFilter] = useState<string>('all');
  const [statusFilter, setStatusFilter] = useState<string>('all');

  const provider = Form.useWatch('provider', form) ?? 'openai';

  const loadAccounts = async () => {
    setLoading(true);
    try {
      const response = await listAccounts();
      setAccounts(response.data);
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadAccounts();
  }, []);

  const filteredAccounts = useMemo(() => {
    const keyword = searchText.trim().toLowerCase();
    return accounts.filter((account) => {
      const matchesKeyword =
        keyword.length === 0 ||
        account.id.toLowerCase().includes(keyword) ||
        account.name.toLowerCase().includes(keyword);
      const matchesProvider = providerFilter === 'all' || account.provider === providerFilter;
      const matchesStatus = statusFilter === 'all' || account.status === statusFilter;
      return matchesKeyword && matchesProvider && matchesStatus;
    });
  }, [accounts, providerFilter, searchText, statusFilter]);

  const openCreateDrawer = () => {
    setEditingAccount(null);
    form.resetFields();
    form.setFieldsValue(getInitialFormValues());
    setDrawerOpen(true);
  };

  const openEditDrawer = (account: Account) => {
    setEditingAccount(account);
    form.resetFields();
    form.setFieldsValue(unpackAccountToForm(account));
    setDrawerOpen(true);
  };

  const closeDrawer = () => {
    setDrawerOpen(false);
    setEditingAccount(null);
    form.resetFields();
  };

  const handleSubmit = async (values: AccountFormValues) => {
    setSubmitting(true);
    try {
      const payload = buildPayload(values);
      if (!payload.api_key) {
        delete payload.api_key;
      }

      if (editingAccount) {
        const previousPayload = buildPayload(unpackAccountToForm(editingAccount));
        delete previousPayload.api_key;
        const changedPayload = diffPayload(payload, previousPayload);
        delete changedPayload.id;

        await updateAccount(editingAccount.id, changedPayload);
        messageApi.success('账号已更新');
      } else {
        await createAccount(payload);
        messageApi.success('账号已创建');
      }

      closeDrawer();
      await loadAccounts();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setSubmitting(false);
    }
  };

  const handleStatusToggle = async (account: Account, checked: boolean) => {
    try {
      await updateAccountStatus(account.id, checked ? 'enabled' : 'disabled');
      messageApi.success(`账号已${checked ? '启用' : '禁用'}`);
      await loadAccounts();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  };

  const handleResetBreaker = (account: Account) => {
    void Modal.confirm({
      title: `重置 ${account.name || account.id} 的熔断器？`,
      content: '该操作会清空当前熔断状态并重新开始探测。',
      okText: '重置',
      cancelText: '取消',
      onOk: async () => {
        try {
          await resetBreaker(account.id);
          messageApi.success('熔断器已重置');
          await loadAccounts();
        } catch (error) {
          messageApi.error(getErrorMessage(error));
          throw error;
        }
      },
    });
  };

  const handleDelete = async (account: Account) => {
    try {
      await deleteAccount(account.id);
      messageApi.success('账号已删除');
      await loadAccounts();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  };

  const columns = useMemo<TableProps<Account>['columns']>(
    () => [
      {
        title: 'ID',
        dataIndex: 'id',
        key: 'id',
      },
      {
        title: '名称',
        dataIndex: 'name',
        key: 'name',
      },
      {
        title: 'Provider',
        dataIndex: 'provider',
        key: 'provider',
        render: (providerValue: Account['provider']) => providerLabelMap[providerValue] ?? providerValue,
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        render: renderAccountStatus,
      },
      {
        title: '熔断器',
        dataIndex: 'breaker_state',
        key: 'breaker_state',
        render: renderBreakerState,
      },
      {
        title: '允许模型',
        dataIndex: 'allowed_models',
        key: 'allowed_models',
        render: renderAllowedModels,
      },
      {
        title: '最大并发',
        dataIndex: 'max_concurrent',
        key: 'max_concurrent',
      },
      {
        title: '操作',
        key: 'actions',
        width: 280,
        render: (_, record) => (
          <Space size="small" onClick={(event) => event.stopPropagation()}>
            <Switch
              checked={record.status === 'enabled'}
              checkedChildren="启用"
              unCheckedChildren="禁用"
              onChange={(checked) => void handleStatusToggle(record, checked)}
            />
            <Button icon={<EditOutlined />} onClick={() => openEditDrawer(record)}>
              编辑
            </Button>
            <Button icon={<ReloadOutlined />} onClick={() => handleResetBreaker(record)}>
              重置熔断
            </Button>
            <Popconfirm
              title={`删除账号 ${record.id}？`}
              description="删除后无法恢复。"
              okText="删除"
              cancelText="取消"
              onConfirm={() => void handleDelete(record)}
            >
              <Button danger icon={<DeleteOutlined />}>
                删除
              </Button>
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [accounts],
  );

  const providerAlert = providerAlertMap[provider];

  return (
    <Space direction="vertical" size={24} style={{ width: '100%' }}>
      {contextHolder}

      <Row align="middle" justify="space-between" gutter={[16, 16]}>
        <Col>
          <Typography.Title level={2} style={{ margin: 0 }}>
            源账号
          </Typography.Title>
        </Col>
        <Col>
          <Button icon={<PlusOutlined />} type="primary" onClick={openCreateDrawer}>
            新建账号
          </Button>
        </Col>
      </Row>

      <Card>
        <Row gutter={[16, 16]}>
          <Col xs={24} md={10}>
            <Input.Search
              allowClear
              placeholder="搜索 ID 或名称"
              value={searchText}
              onChange={(event) => setSearchText(event.target.value)}
            />
          </Col>
          <Col xs={24} md={7}>
            <Select
              style={{ width: '100%' }}
              value={providerFilter}
              options={[{ label: '全部 Provider', value: 'all' }, ...providerOptions]}
              onChange={setProviderFilter}
            />
          </Col>
          <Col xs={24} md={7}>
            <Select
              style={{ width: '100%' }}
              value={statusFilter}
              options={[
                { label: '全部状态', value: 'all' },
                { label: '启用', value: 'enabled' },
                { label: '禁用', value: 'disabled' },
              ]}
              onChange={setStatusFilter}
            />
          </Col>
        </Row>
      </Card>

      <Card>
        <Spin spinning={loading}>
          <Table<Account>
            columns={columns}
            dataSource={filteredAccounts}
            rowKey="id"
            scroll={{ x: 1280 }}
            pagination={{ pageSize: 10, showSizeChanger: false }}
          />
        </Spin>
      </Card>

      <Drawer
        title={editingAccount ? `编辑账号 ${editingAccount.id}` : '新建账号'}
        placement="right"
        width={640}
        open={drawerOpen}
        onClose={closeDrawer}
        destroyOnHidden
        extra={
          <Space>
            <Button onClick={closeDrawer}>取消</Button>
            <Button loading={submitting} type="primary" onClick={() => void form.submit()}>
              {editingAccount ? '保存' : '创建'}
            </Button>
          </Space>
        }
      >
        <Form<AccountFormValues>
          form={form}
          layout="vertical"
          initialValues={getInitialFormValues()}
          onFinish={(values) => void handleSubmit(values)}
        >
          <Alert message={providerAlert.message} description={providerAlert.description} showIcon style={{ marginBottom: 16 }} />

          <Typography.Title level={5}>基本信息</Typography.Title>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item label="ID" name="id">
                <Input disabled={Boolean(editingAccount)} placeholder="留空自动生成" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                label="名称"
                name="name"
                rules={[{ required: true, message: '请输入账号名称' }]}
              >
                <Input />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item
            label="Provider"
            name="provider"
            rules={[{ required: true, message: '请选择 Provider' }]}
          >
            <Select options={providerOptions} />
          </Form.Item>

          <Divider />
          <Typography.Title level={5}>连接配置</Typography.Title>
          <Form.Item
            label="API Key"
            name="api_key"
            rules={editingAccount ? undefined : [{ required: true, message: '请输入 API Key' }]}
          >
            <Input.Password placeholder={editingAccount ? '留空表示不修改' : '请输入 API Key'} />
          </Form.Item>

          <Row gutter={16}>
            <Col span={12}>
              <Form.Item
                label="Base URL"
                name="base_url"
                rules={
                  provider === 'custom_openai' || provider === 'custom_anthropic'
                    ? [{ required: true, message: '该 Provider 必须填写 Base URL' }]
                    : undefined
                }
              >
                <Input placeholder="https://example.com" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="Proxy URL" name="proxy_url">
                <Input placeholder="http://127.0.0.1:7890" />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item label="User-Agent" name="user_agent">
            <Input placeholder="留空表示透传客户端 UA" />
          </Form.Item>

          <Divider />
          <Typography.Title level={5}>Provider 特有配置</Typography.Title>
          {(provider === 'anthropic' || provider === 'custom_anthropic') && (
            <Alert
              type="info"
              showIcon
              message="Anthropic Header 透传"
              description="anthropic-version、anthropic-beta 等 Header 由网关透传，扩展 thinking 配置无需在此页单独填写。"
            />
          )}

          {provider !== 'anthropic' && provider !== 'custom_anthropic' && (
            <Form.Item
              label="思维深度"
              name="thinking_effort"
              tooltip="控制模型推理深度。其他参数（摘要、安全过滤、Tool 过滤等）均已自动设为最优默认值"
            >
              <Select
                allowClear
                placeholder="不设置"
                options={[
                  { label: 'low（快速）', value: 'low' },
                  { label: 'medium（均衡）', value: 'medium' },
                  { label: 'high（深度）', value: 'high' },
                ]}
              />
            </Form.Item>
          )}

          <Divider />
          <Typography.Title level={5}>模型配置</Typography.Title>
          <Form.List name="allowed_models">
            {(fields, { add, remove }) => (
              <Space direction="vertical" style={{ width: '100%' }}>
                {fields.map((field) => (
                  <Space key={field.key} align="start" style={{ display: 'flex' }}>
                    <Form.Item
                      {...field}
                      label={field.name === 0 ? '允许模型' : ''}
                      name={field.name}
                      rules={[]}
                      style={{ flex: 1, marginBottom: 0 }}
                    >
                      <Input placeholder="例如 gpt-5.2" />
                    </Form.Item>
                    <Button danger onClick={() => remove(field.name)}>
                      删除
                    </Button>
                  </Space>
                ))}
                <Button type="dashed" icon={<PlusOutlined />} onClick={() => add('')}>
                  添加模型
                </Button>
              </Space>
            )}
          </Form.List>

          <Divider />
          <Form.List name="model_aliases">
            {(fields, { add, remove }) => (
              <Space direction="vertical" style={{ width: '100%' }}>
                <Typography.Text strong>模型映射</Typography.Text>
                {fields.map((field) => (
                  <Space key={field.key} align="start" style={{ display: 'flex', width: '100%' }}>
                    <Form.Item
                      label={field.name === 0 ? '原模型' : ''}
                      name={[field.name, 'from']}
                      rules={[{ required: true, whitespace: true, message: '请输入原模型名' }]}
                      style={{ flex: 1, marginBottom: 0 }}
                    >
                      <Input placeholder="from" />
                    </Form.Item>
                    <Form.Item
                      label={field.name === 0 ? '映射到' : ''}
                      name={[field.name, 'to']}
                      rules={[{ required: true, whitespace: true, message: '请输入目标模型名' }]}
                      style={{ flex: 1, marginBottom: 0 }}
                    >
                      <Input placeholder="to" />
                    </Form.Item>
                    <Button danger onClick={() => remove(field.name)}>
                      删除
                    </Button>
                  </Space>
                ))}
                <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ from: '', to: '' })}>
                  添加映射
                </Button>
              </Space>
            )}
          </Form.List>

          <Divider />
          <Typography.Title level={5}>限制配置</Typography.Title>
          <Form.Item label="max_concurrent" name="max_concurrent">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>

          <Divider />
          <Typography.Title level={5}>熔断配置</Typography.Title>
          <Row gutter={16}>
            <Col span={8}>
              <Form.Item
                label="failure_threshold"
                name="failure_threshold"
                rules={[{ required: true, message: '请输入失败阈值' }]}
              >
                <InputNumber min={1} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                label="success_threshold"
                name="success_threshold"
                rules={[{ required: true, message: '请输入恢复阈值' }]}
              >
                <InputNumber min={1} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                label="open_duration"
                name="open_duration"
                rules={[{ required: true, message: '请输入开启时长' }]}
              >
                <Input placeholder="60s" />
              </Form.Item>
            </Col>
          </Row>
        </Form>
      </Drawer>
    </Space>
  );
};

export default Accounts;
