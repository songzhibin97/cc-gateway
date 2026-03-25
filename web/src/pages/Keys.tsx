import React, { useEffect, useMemo, useState } from 'react';
import type { AxiosError } from 'axios';
import {
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Progress,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import type { TableProps } from 'antd';
import { CopyOutlined, DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { Link } from 'react-router-dom';
import { createKey, deleteKey, listKeys, rotateKey, updateKey, updateKeyStatus } from '../api/keys';
import { listGroups } from '../api/groups';
import type { APIKey, KeyGroup } from '../types';

interface KeyFormValues {
  id: string;
  group_id: string;
  max_concurrent: number;
  max_input_tokens_monthly: number;
  max_output_tokens_monthly: number;
  allowed_models: string[];
}

interface SecretModalState {
  open: boolean;
  title: string;
  rawKey: string;
}

const getErrorMessage = (error: unknown) => {
  const axiosError = error as AxiosError<{ message?: string; error?: string }>;
  return axiosError.response?.data?.message ?? axiosError.response?.data?.error ?? axiosError.message ?? '请求失败';
};

const formatTokens = (n: number) => {
  if (n >= 1_000_000) {
    return `${(n / 1_000_000).toFixed(1)}M`;
  }
  if (n >= 1_000) {
    return `${(n / 1_000).toFixed(1)}K`;
  }
  return `${n}`;
};

const renderUsageBar = (used: number, limit: number) => {
  const pct = limit > 0 ? Math.min(100, (used / limit) * 100) : 0;
  return (
    <Progress
      percent={pct}
      size="small"
      format={() => (limit > 0 ? `${formatTokens(used)} / ${formatTokens(limit)}` : formatTokens(used))}
    />
  );
};

const getInitialValues = (): KeyFormValues => ({
  id: '',
  group_id: '',
  max_concurrent: 0,
  max_input_tokens_monthly: 0,
  max_output_tokens_monthly: 0,
  allowed_models: [],
});

const unpackKey = (key: APIKey): KeyFormValues => ({
  id: key.id,
  group_id: key.group_id,
  max_concurrent: key.max_concurrent ?? 0,
  max_input_tokens_monthly: key.max_input_tokens_monthly ?? 0,
  max_output_tokens_monthly: key.max_output_tokens_monthly ?? 0,
  allowed_models: key.allowed_models && key.allowed_models.length > 0 ? key.allowed_models : [],
});

const buildPayload = (values: KeyFormValues): Partial<APIKey> => ({
  id: values.id.trim(),
  group_id: values.group_id,
  max_concurrent: values.max_concurrent,
  max_input_tokens_monthly: values.max_input_tokens_monthly,
  max_output_tokens_monthly: values.max_output_tokens_monthly,
  allowed_models: values.allowed_models.map((item) => item.trim()).filter(Boolean),
});

const getCliBaseUrl = () => `${window.location.protocol}//${window.location.hostname}:19999`;

const Keys: React.FC = () => {
  const [form] = Form.useForm<KeyFormValues>();
  const [messageApi, contextHolder] = message.useMessage();
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [groups, setGroups] = useState<KeyGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editingKey, setEditingKey] = useState<APIKey | null>(null);
  const [secretModal, setSecretModal] = useState<SecretModalState>({
    open: false,
    title: '',
    rawKey: '',
  });

  const loadData = async () => {
    setLoading(true);
    try {
      const [keysResponse, groupsResponse] = await Promise.all([listKeys(), listGroups()]);
      setKeys(keysResponse.data);
      setGroups(groupsResponse.data);
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  const groupMap = useMemo(() => new Map(groups.map((group) => [group.id, group])), [groups]);

  const groupOptions = useMemo(
    () =>
      groups.map((group) => ({
        label: `${group.name || group.id} (${group.id})`,
        value: group.id,
      })),
    [groups],
  );

  const openCreateModal = () => {
    setEditingKey(null);
    form.resetFields();
    form.setFieldsValue(getInitialValues());
    setModalOpen(true);
  };

  const openEditModal = (key: APIKey) => {
    setEditingKey(key);
    form.resetFields();
    form.setFieldsValue(unpackKey(key));
    setModalOpen(true);
  };

  const closeModal = () => {
    setModalOpen(false);
    setEditingKey(null);
    form.resetFields();
  };

  const showSecretModal = (title: string, rawKey: string) => {
    setSecretModal({
      open: true,
      title,
      rawKey,
    });
  };

  const copyRawKey = async () => {
    try {
      await navigator.clipboard.writeText(secretModal.rawKey);
      messageApi.success('密钥已复制');
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  };

  const handleSubmit = async (values: KeyFormValues) => {
    setSubmitting(true);
    try {
      const payload = buildPayload(values);
      if (editingKey) {
        await updateKey(editingKey.id, { ...payload, id: undefined });
        messageApi.success('密钥已更新');
      } else {
        const response = await createKey(payload);
        messageApi.success('密钥已创建');
        showSecretModal('密钥已创建', response.data.raw_key);
      }
      closeModal();
      await loadData();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setSubmitting(false);
    }
  };

  const handleStatusToggle = async (key: APIKey, checked: boolean) => {
    try {
      await updateKeyStatus(key.id, checked ? 'enabled' : 'disabled');
      messageApi.success(`密钥已${checked ? '启用' : '禁用'}`);
      await loadData();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  };

  const handleRotate = async (key: APIKey) => {
    try {
      const response = await rotateKey(key.id);
      messageApi.success('密钥已轮换');
      showSecretModal('密钥已轮换', response.data.raw_key);
      await loadData();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  };

  const handleDelete = async (key: APIKey) => {
    try {
      await deleteKey(key.id);
      messageApi.success('密钥已删除');
      await loadData();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  };

  const columns = useMemo<TableProps<APIKey>['columns']>(
    () => [
      {
        title: 'ID',
        dataIndex: 'id',
        key: 'id',
      },
      {
        title: 'Key Hint',
        dataIndex: 'key_hint',
        key: 'key_hint',
        render: (value: string) => `****${value}`,
      },
      {
        title: 'Group',
        dataIndex: 'group_id',
        key: 'group_id',
        render: (groupId: string) => {
          const group = groupMap.get(groupId);
          return (
            <Link to={`/groups#${groupId}`}>
              {group?.name || groupId}
            </Link>
          );
        },
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        render: (status: APIKey['status']) => (
          <Tag color={status === 'enabled' ? 'green' : 'default'}>{status === 'enabled' ? '启用' : '禁用'}</Tag>
        ),
      },
      {
        title: 'Monthly Usage',
        key: 'usage',
        width: 360,
        render: (_, record) => (
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <div>
              <Typography.Text type="secondary">输入 Token</Typography.Text>
              {renderUsageBar(record.used_input_tokens, record.max_input_tokens_monthly)}
              {record.max_input_tokens_monthly === 0 && (
                <Typography.Text type="secondary">不限</Typography.Text>
              )}
            </div>
            <div>
              <Typography.Text type="secondary">输出 Token</Typography.Text>
              {renderUsageBar(record.used_output_tokens, record.max_output_tokens_monthly)}
              {record.max_output_tokens_monthly === 0 && (
                <Typography.Text type="secondary">不限</Typography.Text>
              )}
            </div>
          </Space>
        ),
      },
      {
        title: '操作',
        key: 'actions',
        width: 320,
        render: (_, record) => (
          <Space size="small">
            <Switch
              checked={record.status === 'enabled'}
              checkedChildren="启用"
              unCheckedChildren="禁用"
              onChange={(checked) => void handleStatusToggle(record, checked)}
            />
            <Button icon={<EditOutlined />} onClick={() => openEditModal(record)}>
              编辑
            </Button>
            <Popconfirm
              title={`轮换密钥 ${record.id}？`}
              description="轮换后旧值会立即失效。"
              okText="轮换"
              cancelText="取消"
              onConfirm={() => void handleRotate(record)}
            >
              <Button icon={<ReloadOutlined />}>轮换</Button>
            </Popconfirm>
            <Popconfirm
              title={`删除密钥 ${record.id}？`}
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
    [groupMap],
  );

  return (
    <Space direction="vertical" size={24} style={{ width: '100%' }}>
      {contextHolder}

      <Space align="center" style={{ width: '100%', justifyContent: 'space-between' }}>
        <Typography.Title level={2} style={{ margin: 0 }}>
          API 密钥
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreateModal}>
          新建密钥
        </Button>
      </Space>

      <Card>
        <Table<APIKey>
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={keys}
          pagination={false}
          scroll={{ x: 1100 }}
        />
      </Card>

      <Modal
        title={editingKey ? '编辑密钥' : '新建密钥'}
        open={modalOpen}
        onCancel={closeModal}
        onOk={() => form.submit()}
        confirmLoading={submitting}
        okText={editingKey ? '保存' : '创建'}
        cancelText="取消"
        width={720}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" onFinish={(values) => void handleSubmit(values)}>
          <Form.Item label="ID" name="id">
            <Input disabled={Boolean(editingKey)} placeholder="留空自动生成" />
          </Form.Item>

          <Form.Item label="Group" name="group_id" rules={[{ required: true, message: '请选择账号组' }]}>
            <Select placeholder="选择账号组" options={groupOptions} />
          </Form.Item>

          <Form.Item label="Max Concurrent" name="max_concurrent">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item label="Max Input Tokens Monthly" name="max_input_tokens_monthly">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item label="Max Output Tokens Monthly" name="max_output_tokens_monthly">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>

          <Form.List name="allowed_models">
            {(fields, { add, remove }) => (
              <Space direction="vertical" style={{ width: '100%' }}>
                <Typography.Text strong>Allowed Models</Typography.Text>
                {fields.map((field) => (
                  <Space key={field.key} align="start" style={{ display: 'flex' }}>
                    <Form.Item
                      {...field}
                      name={field.name}
                      rules={[]}
                      style={{ flex: 1, marginBottom: 0 }}
                    >
                      <Input placeholder="例如 claude-sonnet-4-5" />
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
        </Form>
      </Modal>

      <Modal
        title={secretModal.title}
        open={secretModal.open}
        onOk={() => setSecretModal((prev) => ({ ...prev, open: false }))}
        onCancel={() => setSecretModal((prev) => ({ ...prev, open: false }))}
        okText="确定"
        cancelButtonProps={{ style: { display: 'none' } }}
      >
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <div
            style={{
              padding: 16,
              borderRadius: 8,
              background: 'rgba(0, 0, 0, 0.04)',
              border: '1px solid rgba(0, 0, 0, 0.08)',
            }}
          >
            <Typography.Text code copyable={false} style={{ fontSize: 14, wordBreak: 'break-all' }}>
              {secretModal.rawKey}
            </Typography.Text>
          </div>
          <Button icon={<CopyOutlined />} onClick={() => void copyRawKey()}>
            复制密钥
          </Button>
          <div>
            <Typography.Text strong>CLI 配置提示</Typography.Text>
            <Typography.Paragraph code style={{ marginTop: 8, marginBottom: 0 }}>
              {`ANTHROPIC_BASE_URL=${getCliBaseUrl()}\nANTHROPIC_API_KEY=${secretModal.rawKey || 'sk-xxx'}`}
            </Typography.Paragraph>
          </div>
          <Typography.Text type="warning">关闭后无法再次查看</Typography.Text>
        </Space>
      </Modal>
    </Space>
  );
};

export default Keys;
