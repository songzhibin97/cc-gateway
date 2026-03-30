import React, { useCallback, useEffect, useMemo, useState } from 'react';
import type { AxiosError } from 'axios';
import {
  Button,
  Card,
  Checkbox,
  Col,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Radio,
  Row,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';
import { createGroup, deleteGroup, listGroups, updateGroup } from '../api/groups';
import { listAccounts } from '../api/accounts';
import { listKeys } from '../api/keys';
import { providerLabelMap, renderAccountStatus, renderBreakerState } from '../components/accountUi';
import type { Account, APIKey, KeyGroup } from '../types';

interface GroupFormValues {
  id: string;
  name: string;
  balancer: KeyGroup['balancer'];
  account_ids: string[];
  allowed_models: string[];
}

const balancerOptions = [
  {
    label: '轮询',
    value: 'round_robin',
    description: '按顺序均匀分配请求，适合稳定同质账号。',
  },
  {
    label: '最少连接',
    value: 'least_connections',
    description: '优先选择当前活跃请求更少的账号。',
  },
  {
    label: '加权',
    value: 'weighted',
    description: '按账号权重倾斜流量，适合主备或异构池。',
  },
  {
    label: '优先级',
    value: 'priority',
    description: '按账号顺序优先选择，只有不可用时才用下一个，适合主备场景。',
  },
] as const;

const balancerLabelMap = Object.fromEntries(
  balancerOptions.map((option) => [option.value, option.label]),
) as Record<string, string>;

const getErrorMessage = (error: unknown) => {
  const axiosError = error as AxiosError<{ message?: string; error?: string }>;
  return axiosError.response?.data?.message ?? axiosError.response?.data?.error ?? axiosError.message ?? '请求失败';
};

const getInitialValues = (): GroupFormValues => ({
  id: '',
  name: '',
  balancer: 'round_robin',
  account_ids: [],
  allowed_models: [],
});

const unpackGroup = (group: KeyGroup): GroupFormValues => ({
  id: group.id,
  name: group.name ?? '',
  balancer: group.balancer || 'round_robin',
  account_ids: group.account_ids ?? [],
  allowed_models: group.allowed_models && group.allowed_models.length > 0 ? group.allowed_models : [],
});

const buildPayload = (values: GroupFormValues): Partial<KeyGroup> => ({
  id: values.id.trim(),
  name: values.name.trim(),
  balancer: values.balancer,
  account_ids: values.account_ids,
  allowed_models: values.allowed_models.map((item) => item.trim()).filter(Boolean),
});

const renderModels = (models: string[] | null) => {
  if (!models || models.length === 0) {
    return <Typography.Text type="secondary">未限制</Typography.Text>;
  }

  return (
    <Space size={[6, 6]} wrap>
      {models.map((model) => (
        <Tag key={model}>{model}</Tag>
      ))}
    </Space>
  );
};

const Groups: React.FC = () => {
  const [form] = Form.useForm<GroupFormValues>();
  const [messageApi, contextHolder] = message.useMessage();
  const [modal, modalContextHolder] = Modal.useModal();
  const [groups, setGroups] = useState<KeyGroup[]>([]);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<KeyGroup | null>(null);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [groupsResponse, accountsResponse, keysResponse] = await Promise.all([
        listGroups(),
        listAccounts(),
        listKeys(),
      ]);
      setGroups(groupsResponse.data);
      setAccounts(accountsResponse.data);
      setKeys(keysResponse.data);
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setLoading(false);
    }
  }, [messageApi]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const accountMap = useMemo(
    () => new Map(accounts.map((account) => [account.id, account])),
    [accounts],
  );

  const keyCountByGroup = useMemo(() => {
    const counts = new Map<string, number>();
    keys.forEach((key) => {
      counts.set(key.group_id, (counts.get(key.group_id) ?? 0) + 1);
    });
    return counts;
  }, [keys]);

  const accountOptions = useMemo(
    () =>
      accounts.map((account) => ({
        label: (
          <Space wrap size={[8, 4]}>
            <Typography.Text strong>{account.id}</Typography.Text>
            <Tag>{providerLabelMap[account.provider] ?? account.provider}</Tag>
            {renderAccountStatus(account.status)}
            {renderBreakerState(account.breaker_state)}
          </Space>
        ),
        value: account.id,
      })),
    [accounts],
  );

  const openCreateModal = () => {
    setEditingGroup(null);
    form.resetFields();
    form.setFieldsValue(getInitialValues());
    setModalOpen(true);
  };

  const openEditModal = (group: KeyGroup) => {
    setEditingGroup(group);
    form.resetFields();
    form.setFieldsValue(unpackGroup(group));
    setModalOpen(true);
  };

  const closeModal = () => {
    setModalOpen(false);
    setEditingGroup(null);
    form.resetFields();
  };

  const handleSubmit = async (values: GroupFormValues) => {
    setSubmitting(true);
    try {
      const payload = buildPayload(values);
      if (editingGroup) {
        await updateGroup(editingGroup.id, { ...payload, id: undefined });
        messageApi.success('账号组已更新');
      } else {
        await createGroup(payload);
        messageApi.success('账号组已创建');
      }
      closeModal();
      await loadData();
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (group: KeyGroup) => {
    try {
      await deleteGroup(group.id);
      messageApi.success('账号组已删除');
      await loadData();
    } catch (error) {
      const axiosError = error as AxiosError<{ message?: string; error?: string }>;
      if (axiosError.response?.status === 409) {
        modal.error({
          title: '无法删除账号组',
          content:
            axiosError.response?.data?.message ??
            axiosError.response?.data?.error ??
            '该账号组仍被 API 密钥引用，请先解绑或删除相关密钥。',
        });
        return;
      }
      messageApi.error(getErrorMessage(error));
    }
  };

  if (loading) {
    return (
      <Space align="center" style={{ width: '100%', justifyContent: 'center', padding: '120px 0' }}>
        <Spin size="large" />
      </Space>
    );
  }

  return (
    <Space direction="vertical" size={24} style={{ width: '100%' }}>
      {contextHolder}
      {modalContextHolder}

      <Row align="middle" justify="space-between" gutter={[16, 16]}>
        <Col>
          <Typography.Title level={2} style={{ margin: 0 }}>
            账号组
          </Typography.Title>
        </Col>
        <Col>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreateModal}>
            新建组
          </Button>
        </Col>
      </Row>

      {groups.length === 0 ? (
        <Card>
          <Empty description="暂无账号组" />
        </Card>
      ) : (
        <Row gutter={[16, 16]}>
          {groups.map((group) => (
            <Col key={group.id} xs={24} lg={12} xxl={8}>
              <Card
                id={group.id}
                title={
                  <Space direction="vertical" size={2}>
                    <Typography.Text strong style={{ fontSize: 16 }}>
                      {group.name || group.id}
                    </Typography.Text>
                    <Typography.Text type="secondary">
                      {balancerLabelMap[group.balancer] ?? group.balancer}
                    </Typography.Text>
                  </Space>
                }
                extra={<Typography.Text type="secondary">{group.id}</Typography.Text>}
                actions={[
                  <Button key="edit" type="link" icon={<EditOutlined />} onClick={() => openEditModal(group)}>
                    编辑
                  </Button>,
                  <Popconfirm
                    key="delete"
                    title={`删除账号组 ${group.id}？`}
                    description="删除后无法恢复。若仍被密钥引用，系统会阻止删除。"
                    okText="删除"
                    cancelText="取消"
                    onConfirm={() => void handleDelete(group)}
                  >
                    <Button type="link" danger icon={<DeleteOutlined />}>
                      删除
                    </Button>
                  </Popconfirm>,
                ]}
              >
                <Space direction="vertical" size={16} style={{ width: '100%' }}>
                  <div>
                    <Typography.Text strong>账号池</Typography.Text>
                    <div style={{ marginTop: 10 }}>
                      {group.account_ids.length === 0 ? (
                        <Typography.Text type="secondary">未绑定账号</Typography.Text>
                      ) : (
                        <Space size={[8, 8]} wrap>
                          {group.account_ids.map((accountId) => {
                            const account = accountMap.get(accountId);
                            if (!account) {
                              return (
                                <Tag key={accountId} color="default">
                                  {accountId}
                                </Tag>
                              );
                            }

                            return (
                              <Tag key={accountId} style={{ padding: '6px 8px' }}>
                                <Space size={6} wrap>
                                  <Typography.Text>{account.id}</Typography.Text>
                                  <Tag bordered={false}>{providerLabelMap[account.provider] ?? account.provider}</Tag>
                                  {renderAccountStatus(account.status)}
                                  {renderBreakerState(account.breaker_state)}
                                </Space>
                              </Tag>
                            );
                          })}
                        </Space>
                      )}
                    </div>
                  </div>

                  <div>
                    <Typography.Text strong>摘要</Typography.Text>
                    <Space direction="vertical" size={6} style={{ display: 'flex', marginTop: 8 }}>
                      <Typography.Text>绑定密钥: {keyCountByGroup.get(group.id) ?? 0}</Typography.Text>
                      <div>
                        <Typography.Text>允许模型: </Typography.Text>
                        {renderModels(group.allowed_models)}
                      </div>
                    </Space>
                  </div>
                </Space>
              </Card>
            </Col>
          ))}
        </Row>
      )}

      <Modal
        title={editingGroup ? '编辑账号组' : '新建账号组'}
        open={modalOpen}
        onCancel={closeModal}
        onOk={() => form.submit()}
        confirmLoading={submitting}
        okText={editingGroup ? '保存' : '创建'}
        cancelText="取消"
        width={760}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" onFinish={(values) => void handleSubmit(values)}>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item label="ID" name="id">
                <Input disabled={Boolean(editingGroup)} placeholder="留空自动生成" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                label="名称"
                name="name"
                rules={[{ required: true, whitespace: true, message: '请输入组名称' }]}
              >
                <Input placeholder="例如 默认账号组" />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item label="负载策略" name="balancer">
            <Radio.Group style={{ width: '100%' }}>
              <Space direction="vertical" style={{ width: '100%' }}>
                {balancerOptions.map((option) => (
                  <Radio key={option.value} value={option.value}>
                    <Space direction="vertical" size={0}>
                      <Typography.Text>{option.label}</Typography.Text>
                      <Typography.Text type="secondary">{option.description}</Typography.Text>
                    </Space>
                  </Radio>
                ))}
              </Space>
            </Radio.Group>
          </Form.Item>

          <Form.Item label="选择账号" name="account_ids">
            <Checkbox.Group options={accountOptions} style={{ width: '100%' }} />
          </Form.Item>

          <Form.List name="allowed_models">
            {(fields, { add, remove }) => (
              <Space direction="vertical" style={{ width: '100%' }}>
                <Typography.Text strong>允许模型</Typography.Text>
                {fields.map((field) => (
                  <Space key={field.key} align="start" style={{ display: 'flex' }}>
                    <Form.Item
                      {...field}
                      name={field.name}
                      rules={[]}
                      style={{ flex: 1, marginBottom: 0 }}
                    >
                      <Input placeholder="例如 claude-opus-4-1" />
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
    </Space>
  );
};

export default Groups;
