import React, { useCallback, useEffect, useMemo, useState } from 'react';
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
  Tooltip,
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
  max_retries?: number;
  retry_base_delay?: string;
  retryable_status_codes?: number[];
}

const defaultThinkingEffortOptions = [
  { label: 'low（快速）', value: 'low' },
  { label: 'medium（均衡）', value: 'medium' },
  { label: 'high（深度）', value: 'high' },
];

const openaiThinkingEffortOptions = [
  ...defaultThinkingEffortOptions,
  { label: 'xhigh（超深度）', value: 'xhigh' },
];

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
  max_retries: 0,
  retry_base_delay: '1s',
  retryable_status_codes: [],
});

const toAliasArray = (aliases: Account['model_aliases']) =>
  Object.entries(aliases ?? {}).map(([from, to]) => ({ from, to }));

const stringOrUndefined = (value: unknown) => (typeof value === 'string' ? value : undefined);

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
    thinking_effort: stringOrUndefined(extra.thinking_effort) ?? stringOrUndefined(extra.reasoning_effort) ?? '',
    ...(() => {
      const retryConfig = (extra.retry_config ?? {}) as Record<string, unknown>;
      return {
        max_retries: (retryConfig.max_retries as number) ?? 0,
        retry_base_delay: (retryConfig.retry_base_delay as string) ?? '1s',
        retryable_status_codes: ((retryConfig.retryable_status_codes ?? []) as unknown[]).map(Number),
      };
    })(),
  };
};

const packExtra = (values: AccountFormValues) => {
  const extra: Record<string, unknown> = {};

  if (values.thinking_effort && values.thinking_effort !== '') {
    extra.thinking_effort = values.thinking_effort;
  }

  const retryConfig: Record<string, unknown> = {};
  if (values.max_retries && values.max_retries > 0) {
    retryConfig.max_retries = values.max_retries;
  }
  if (values.retry_base_delay && values.retry_base_delay !== '' && values.retry_base_delay !== '1s') {
    retryConfig.retry_base_delay = values.retry_base_delay;
  }
  if (values.retryable_status_codes && values.retryable_status_codes.length > 0) {
    retryConfig.retryable_status_codes = values.retryable_status_codes.map(Number);
  }
  if (Object.keys(retryConfig).length > 0) {
    extra.retry_config = retryConfig;
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
  const thinkingEffortOptions =
    provider === 'openai' || provider === 'custom_openai'
      ? openaiThinkingEffortOptions
      : defaultThinkingEffortOptions;

  const loadAccounts = useCallback(async () => {
    setLoading(true);
    try {
      const response = await listAccounts();
      setAccounts(response.data);
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setLoading(false);
    }
  }, [messageApi]);

  useEffect(() => {
    void loadAccounts();
  }, [loadAccounts]);

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

  const openCreateDrawer = useCallback(() => {
    setEditingAccount(null);
    form.resetFields();
    form.setFieldsValue(getInitialFormValues());
    setDrawerOpen(true);
  }, [form]);

  const openEditDrawer = useCallback((account: Account) => {
    setEditingAccount(account);
    form.resetFields();
    form.setFieldsValue(unpackAccountToForm(account));
    setDrawerOpen(true);
  }, [form]);

  const closeDrawer = useCallback(() => {
    setDrawerOpen(false);
    setEditingAccount(null);
    form.resetFields();
  }, [form]);

  const handleSubmit = useCallback(async (values: AccountFormValues) => {
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
  }, [closeDrawer, editingAccount, loadAccounts, messageApi]);

  const handleStatusToggle = useCallback(async (account: Account, checked: boolean) => {
    try {
      await updateAccountStatus(account.id, checked ? 'enabled' : 'disabled');
      messageApi.success(`账号已${checked ? '启用' : '禁用'}`);
      await loadAccounts();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  }, [loadAccounts, messageApi]);

  const handleResetBreaker = useCallback((account: Account) => {
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
  }, [loadAccounts, messageApi]);

  const handleDelete = useCallback(async (account: Account) => {
    try {
      await deleteAccount(account.id);
      messageApi.success('账号已删除');
      await loadAccounts();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  }, [loadAccounts, messageApi]);

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
        title: <Tooltip title="closed=正常放行，open=已熔断拦截，half_open=试探恢复">熔断状态</Tooltip>,
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
    [handleDelete, handleResetBreaker, handleStatusToggle, openEditDrawer],
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
                options={thinkingEffortOptions}
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
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 16 }}
            message="术语说明"
            description="这里遵循标准熔断器语义：closed 表示正常放行，open 表示已熔断拦截，half_open 表示试探恢复。open 不是“正常开放”。"
          />
          <Row gutter={16}>
            <Col span={8}>
              <Form.Item
                label="失败阈值"
                name="failure_threshold"
                tooltip="closed 状态下连续失败达到该值后，熔断器进入已熔断（open）。"
                rules={[{ required: true, message: '请输入失败阈值' }]}
              >
                <InputNumber min={1} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                label="恢复阈值"
                name="success_threshold"
                tooltip="half_open 状态下连续成功达到该值后，熔断器恢复为正常（closed）。"
                rules={[{ required: true, message: '请输入恢复阈值' }]}
              >
                <InputNumber min={1} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                label="熔断时长"
                name="open_duration"
                tooltip="进入 open 后保持多久，时间到了才进入 half_open 试探恢复。支持 60s、5m、1h。"
                rules={[{ required: true, message: '请输入开启时长' }]}
              >
                <Input placeholder="60s" />
              </Form.Item>
            </Col>
          </Row>

          <Divider />
          <Typography.Title level={5}>重试配置</Typography.Title>
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 16 }}
            message="错误重试"
            description="请求失败后在同一账号上重试，使用指数退避策略（base × 2^attempt）避免触发限流。重试全部失败后才会切换到下一个账号。"
          />
          <Row gutter={16}>
            <Col span={8}>
              <Form.Item
                label="最大重试次数"
                name="max_retries"
                tooltip="同一账号上的最大重试次数，0 表示不重试。"
              >
                <InputNumber min={0} max={10} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                label="基础退避延迟"
                name="retry_base_delay"
                tooltip="首次重试的等待时间，后续重试翻倍增长。支持 100ms、1s、5s 等格式。"
              >
                <Input placeholder="1s" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                label="可重试状态码"
                name="retryable_status_codes"
                tooltip="哪些 HTTP 状态码触发重试。留空则使用默认值 500/502/503/504。网络超时/连接错误始终会重试。"
              >
                <Select
                  mode="tags"
                  placeholder="500, 502, 503, 504"
                  tokenSeparators={[',', ' ']}
                  options={[
                    { label: '500', value: 500 },
                    { label: '502', value: 502 },
                    { label: '503', value: 503 },
                    { label: '504', value: 504 },
                    { label: '408', value: 408 },
                    { label: '429', value: 429 },
                  ]}
                />
              </Form.Item>
            </Col>
          </Row>
        </Form>
      </Drawer>
    </Space>
  );
};

export default Accounts;
