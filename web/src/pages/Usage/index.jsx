/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useMemo, useState } from 'react';
import {
  Button,
  DatePicker,
  Descriptions,
  Empty,
  Input,
  Select,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { Eye, EyeOff, KeyRound, Search } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import CardPro from '../../components/common/ui/CardPro';
import CardTable from '../../components/common/ui/CardTable';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import {
  createCardProPagination,
  getLogOther,
  renderQuota,
  showError,
  timestamp2string,
} from '../../helpers';

const { Text } = Typography;

const LOG_TYPE_OPTIONS = [
  { value: 0, label: '全部类型' },
  { value: 2, label: '消费' },
  { value: 5, label: '错误' },
  { value: 6, label: '退款' },
];

function toUnixSeconds(value) {
  if (!value) return 0;
  if (typeof value === 'number') return Math.floor(value / 1000);
  const date = value instanceof Date ? value : new Date(value);
  const time = date.getTime();
  if (!Number.isFinite(time)) return 0;
  return Math.floor(time / 1000);
}

function renderLogType(type, t) {
  switch (type) {
    case 2:
      return (
        <Tag color='lime' shape='circle'>
          {t('消费')}
        </Tag>
      );
    case 5:
      return (
        <Tag color='red' shape='circle'>
          {t('错误')}
        </Tag>
      );
    case 6:
      return (
        <Tag color='teal' shape='circle'>
          {t('退款')}
        </Tag>
      );
    default:
      return (
        <Tag color='grey' shape='circle'>
          {t('未知')}
        </Tag>
      );
  }
}

function buildBillingSummary(other, t) {
  const lines = [];
  if (other?.model_ratio !== undefined) {
    lines.push(`${t('模型倍率')}：${other.model_ratio}`);
  }
  if (other?.completion_ratio !== undefined) {
    lines.push(`${t('补全倍率')}：${other.completion_ratio}`);
  }
  if (other?.group_ratio !== undefined) {
    lines.push(`${t('分组倍率')}：${other.group_ratio}`);
  }
  if (other?.cache_ratio !== undefined && other?.cache_tokens > 0) {
    lines.push(`${t('缓存倍率')}：${other.cache_ratio}`);
  }
  if (other?.image) {
    lines.push(
      `${t('图片计费')}：${other.image_output || '-'} / ${other.image_ratio || '-'}`,
    );
  }
  return lines.join('\n');
}

function buildExpandData(record, t) {
  const other = getLogOther(record.other) || {};
  const data = [];

  if (other.request_path) {
    data.push({ key: t('请求路径'), value: other.request_path });
  }
  if (record.content) {
    data.push({ key: t('日志详情'), value: record.content });
  }

  const billingSummary = buildBillingSummary(other, t);
  if (billingSummary) {
    data.push({
      key: t('计费过程'),
      value: <div style={{ whiteSpace: 'pre-line' }}>{billingSummary}</div>,
    });
  }

  if (Array.isArray(other.request_conversion)) {
    data.push({
      key: t('请求转换'),
      value:
        other.request_conversion.length > 1
          ? other.request_conversion.join(' -> ')
          : t('原生格式'),
    });
  }

  return data;
}

const Usage = () => {
  const { t } = useTranslation();
  const isMobile = useIsMobile();
  const [apiKey, setApiKey] = useState('');
  const [showKey, setShowKey] = useState(false);
  const [modelName, setModelName] = useState('');
  const [requestId, setRequestId] = useState('');
  const [logType, setLogType] = useState(0);
  const [dateRange, setDateRange] = useState([]);
  const [usage, setUsage] = useState(null);
  const [logs, setLogs] = useState([]);
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [logCount, setLogCount] = useState(0);
  const [queried, setQueried] = useState(false);
  const [loading, setLoading] = useState(false);

  const queryUsage = async (page = 1, size = pageSize) => {
    if (loading) return;
    const key = apiKey.trim();
    if (!key) {
      showError(t('请输入 API Key'));
      return;
    }

    setLoading(true);
    const payload = {
      api_key: key,
      p: page,
      page_size: size,
      type: logType,
      start_timestamp: Array.isArray(dateRange)
        ? toUnixSeconds(dateRange[0])
        : 0,
      end_timestamp: Array.isArray(dateRange) ? toUnixSeconds(dateRange[1]) : 0,
      model_name: modelName.trim(),
      request_id: requestId.trim(),
    };

    try {
      const res = await fetch('/api/usage/token/query', {
        method: 'POST',
        credentials: 'omit',
        cache: 'no-store',
        headers: {
          'Content-Type': 'application/json',
          'Cache-Control': 'no-store',
        },
        body: JSON.stringify(payload),
      });
      const { success, message, data } = await res.json();
      if (!success) {
        showError(message || t('API Key 无效或不可查询'));
        setQueried(true);
        setUsage(null);
        setLogs([]);
        setLogCount(0);
        return;
      }

      setUsage(data.usage);
      setLogs(data.logs?.items || []);
      setActivePage(data.logs?.page || page);
      setPageSize(data.logs?.page_size || size);
      setLogCount(data.logs?.total || 0);
      setQueried(true);
    } catch (error) {
      showError(t('API Key 无效或不可查询'));
    } finally {
      setLoading(false);
    }
  };

  const columns = useMemo(
    () => [
      {
        title: t('时间'),
        dataIndex: 'created_at',
        key: 'time',
        width: 170,
        render: (value) => timestamp2string(value),
      },
      {
        title: t('模型'),
        dataIndex: 'model_name',
        key: 'model',
        width: 110,
        render: (value) =>
          value ? (
            <Tag color='blue' shape='circle'>
              {value}
            </Tag>
          ) : (
            '-'
          ),
      },
      {
        title: t('分组'),
        dataIndex: 'group',
        key: 'group',
        width: 230,
        render: (value) =>
          value ? (
            <Tag color='grey' shape='circle' style={{ maxWidth: '100%' }}>
              {value}
            </Tag>
          ) : (
            '-'
          ),
      },
      {
        title: t('请求类型'),
        dataIndex: 'type',
        key: 'type',
        width: 100,
        render: (value) => renderLogType(value, t),
      },
      {
        title: t('输入 Tokens'),
        dataIndex: 'prompt_tokens',
        key: 'prompt_tokens',
        width: 120,
      },
      {
        title: t('输出 Tokens'),
        dataIndex: 'completion_tokens',
        key: 'completion_tokens',
        width: 120,
      },
      {
        title: t('消耗额度'),
        dataIndex: 'quota',
        key: 'quota',
        width: 110,
        render: (value) => renderQuota(value),
      },
      {
        title: t('耗时'),
        dataIndex: 'use_time',
        key: 'use_time',
        width: 90,
        render: (value) => `${value || 0} s`,
      },
      {
        title: t('Request ID'),
        dataIndex: 'request_id',
        key: 'request_id',
        width: 260,
        render: (value) =>
          value ? (
            <Text
              copyable={{ content: value }}
              ellipsis
              style={{ display: 'inline-block', maxWidth: '100%' }}
            >
              {value}
            </Text>
          ) : (
            '-'
          ),
      },
    ],
    [t],
  );

  const statsArea = usage ? (
    <div className='flex flex-col gap-2'>
      <div className='flex flex-wrap gap-2'>
        <Tag color='blue' className='!rounded-lg' style={{ padding: 13 }}>
          {t('令牌名称')}: {usage.name || '-'}
        </Tag>
        <Tag color='green' className='!rounded-lg' style={{ padding: 13 }}>
          {t('总额度')}:{' '}
          {usage.unlimited_quota
            ? t('无限额度')
            : renderQuota(usage.total_granted)}
        </Tag>
        <Tag color='pink' className='!rounded-lg' style={{ padding: 13 }}>
          {t('已用额度')}: {renderQuota(usage.total_used)}
        </Tag>
        <Tag color='grey' className='!rounded-lg' style={{ padding: 13 }}>
          {t('剩余额度')}:{' '}
          {usage.unlimited_quota
            ? t('无限额度')
            : renderQuota(usage.total_available)}
        </Tag>
        <Tag color='grey' className='!rounded-lg' style={{ padding: 13 }}>
          {t('过期时间')}:{' '}
          {usage.expires_at
            ? timestamp2string(usage.expires_at)
            : t('永不过期')}
        </Tag>
      </div>
    </div>
  ) : (
    <Text type='secondary'>{t('用量查询')}</Text>
  );

  const searchArea = (
    <div className='flex flex-col gap-3'>
      <div className='grid grid-cols-1 lg:grid-cols-12 gap-2'>
        <Input
          className='w-full min-w-0 lg:col-span-6'
          type={showKey ? 'text' : 'password'}
          name='token1688-usage-api-key'
          value={apiKey}
          onChange={setApiKey}
          autoComplete='new-password'
          autoCorrect='off'
          autoCapitalize='off'
          spellCheck={false}
          placeholder='sk-...'
          prefix={
            <span
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                margin: '0 5px',
                flexShrink: 0,
              }}
            >
              <KeyRound size={16} />
            </span>
          }
          suffix={
            <Button
              type='tertiary'
              theme='borderless'
              size='small'
              icon={showKey ? <EyeOff size={16} /> : <Eye size={16} />}
              onClick={() => setShowKey((value) => !value)}
            />
          }
          onEnterPress={() => queryUsage(1, pageSize)}
        />
        <DatePicker
          className='w-full min-w-0 lg:col-span-3'
          type='dateTimeRange'
          value={dateRange}
          onChange={setDateRange}
          placeholder={[t('开始时间'), t('结束时间')]}
          showClear
        />
        <Select
          className='w-full min-w-0 lg:col-span-3'
          value={logType}
          optionList={LOG_TYPE_OPTIONS.map((item) => ({
            value: item.value,
            label: t(item.label),
          }))}
          onChange={setLogType}
        />
      </div>
      <div className='grid grid-cols-1 lg:grid-cols-12 gap-2'>
        <Input
          className='w-full min-w-0 lg:col-span-4'
          name='token1688-usage-model-name'
          value={modelName}
          onChange={setModelName}
          autoComplete='off'
          autoCorrect='off'
          autoCapitalize='off'
          spellCheck={false}
          placeholder={t('模型名称')}
          showClear
        />
        <Input
          className='w-full min-w-0 lg:col-span-5'
          name='token1688-usage-request-id'
          value={requestId}
          onChange={setRequestId}
          autoComplete='new-password'
          autoCorrect='off'
          autoCapitalize='off'
          spellCheck={false}
          placeholder={t('Request ID')}
          showClear
        />
        <Button
          className='w-full min-w-0 lg:col-span-3'
          theme='solid'
          type='primary'
          icon={<Search size={16} />}
          loading={loading}
          onClick={() => queryUsage(1, pageSize)}
        >
          {t('查询')}
        </Button>
      </div>
    </div>
  );

  const expandRowRender = (record) => {
    const data = buildExpandData(record, t);
    return data.length > 0 ? <Descriptions data={data} /> : null;
  };

  return (
    <div className='usage-query-page mt-[60px] px-2 md:px-6 pb-6 w-full min-w-0'>
      <CardPro
        className='w-full min-w-0'
        type='type2'
        statsArea={statsArea}
        searchArea={searchArea}
        paginationArea={createCardProPagination({
          currentPage: activePage,
          pageSize,
          total: logCount,
          onPageChange: (page) => queryUsage(page, pageSize),
          onPageSizeChange: (size) => queryUsage(1, size),
          isMobile,
          t,
        })}
        t={t}
      >
        <div className='w-full min-w-0 overflow-x-auto'>
          <CardTable
            className='rounded-xl overflow-hidden w-full min-w-[1310px]'
            columns={columns}
            dataSource={logs}
            rowKey='id'
            loading={loading}
            size='small'
            hidePagination
            expandedRowRender={expandRowRender}
            rowExpandable={(record) => buildExpandData(record, t).length > 0}
            empty={
              <Empty
                image={
                  <IllustrationNoResult style={{ width: 150, height: 150 }} />
                }
                darkModeImage={
                  <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
                }
                description={queried ? t('搜索无结果') : t('暂无数据')}
                style={{ padding: 30 }}
              />
            }
          />
        </div>
      </CardPro>
    </div>
  );
};

export default Usage;
