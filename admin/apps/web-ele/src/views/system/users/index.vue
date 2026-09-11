<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type { FormInstance, FormRules } from 'element-plus';

import type {
  SystemUserPayload,
  SystemUserRecord,
  SystemUserStatus,
} from '#/api/system/user';

import { computed, onMounted, reactive, ref } from 'vue';

import dayjs from 'dayjs';
import {
  ElButton,
  ElCard,
  ElDialog,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElOption,
  ElPagination,
  ElSelect,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import {
  createSystemUserApi,
  getSystemUsersApi,
  resetSystemUserPasswordApi,
  updateSystemUserApi,
} from '#/api/system/user';

import { buildUserListParams, createUserForm, toUserForm } from './model';

defineOptions({ name: 'SystemUsers' });
const users = ref<SystemUserRecord[]>([]);
const loading = ref(false);
const dialogVisible = ref(false);
const submitting = ref(false);
const editingId = ref<number>();
const formRef = ref<FormInstance>();
const query = reactive<{ keyword: string; status: '' | SystemUserStatus }>({
  keyword: '',
  status: '',
});
const pagination = reactive({ page: 1, pageSize: 20, total: 0 });
const form = reactive<SystemUserPayload>(createUserForm());
const resetTarget = ref<SystemUserRecord>();
const resetPassword = ref('');
const resetVisible = ref(false);
const formRules = computed<FormRules>(() => ({
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    {
      min: 3,
      max: 32,
      message: '用户名长度应为 3 至 32 个字符',
      trigger: 'blur',
    },
  ],
  realName: [{ required: true, message: '请输入姓名', trigger: 'blur' }],
  password: editingId.value
    ? []
    : [
        {
          required: true,
          min: 8,
          message: '密码至少 8 个字符',
          trigger: 'blur',
        },
      ],
}));

async function load() {
  loading.value = true;
  try {
    const data = await getSystemUsersApi(
      buildUserListParams(query, pagination.page, pagination.pageSize),
    );
    users.value = data.items;
    pagination.total = data.total;
  } finally {
    loading.value = false;
  }
}
function openCreate() {
  editingId.value = undefined;
  Object.assign(form, createUserForm());
  dialogVisible.value = true;
}
function openEdit(user: SystemUserRecord) {
  editingId.value = user.id;
  Object.assign(form, toUserForm(user));
  dialogVisible.value = true;
}
async function submit() {
  if (!(await formRef.value?.validate().catch(() => false))) return;
  submitting.value = true;
  try {
    if (editingId.value) {
      await updateSystemUserApi(editingId.value, form);
      ElMessage.success('用户已更新');
    } else {
      await createSystemUserApi(form);
      ElMessage.success('用户已创建');
    }
    dialogVisible.value = false;
    await load();
  } finally {
    submitting.value = false;
  }
}
function openReset(user: SystemUserRecord) {
  resetTarget.value = user;
  resetPassword.value = '';
  resetVisible.value = true;
}
async function confirmReset() {
  if (resetPassword.value.length < 8 || !resetTarget.value) {
    ElMessage.warning('密码至少 8 个字符');
    return;
  }
  await resetSystemUserPasswordApi(resetTarget.value.id, resetPassword.value);
  ElMessage.success('密码已重置，原登录会话已撤销');
  resetVisible.value = false;
}
function search() {
  pagination.page = 1;
  void load();
}
onMounted(load);
</script>

<template>
  <div class="wp-page">
    <ElCard class="wp-page-hero" shadow="never">
      <div class="wp-page-hero__content">
        <div>
          <p class="wp-page-eyebrow">TEAM MEMBERS</p>
          <h1 class="wp-page-title">用户管理</h1>
          <p class="wp-page-description">
            创建内部成员，并分配管理员或编辑角色。
          </p>
        </div>
        <ElButton
          v-access:code="'user:create'"
          class="wp-page-action"
          size="large"
          type="primary"
          @click="openCreate"
        >
          新增用户
        </ElButton>
      </div>
    </ElCard>
    <ElCard class="wp-panel wp-filter-panel" shadow="never">
      <div class="wp-filter-bar">
        <span class="wp-filter-bar__label">筛选条件</span>
        <div class="wp-filter-bar__controls">
          <ElInput
            v-model="query.keyword"
            clearable
            class="w-72"
            placeholder="搜索用户名或姓名"
            @keyup.enter="search"
          /><ElSelect
            v-model="query.status"
            clearable
            class="w-40"
            placeholder="全部状态"
          >
            <ElOption label="启用" value="active" /><ElOption
              label="停用"
              value="disabled"
            /> </ElSelect
          ><ElButton type="primary" @click="search">查询</ElButton>
        </div>
      </div>
    </ElCard>
    <ElCard class="wp-panel wp-table-panel" shadow="never">
      <div class="wp-table-panel__header">
        <div>
          <p class="wp-panel-title">成员列表</p>
          <p class="wp-panel-description">管理团队账号、角色和启用状态</p>
        </div>
        <span class="wp-record-count">共 {{ pagination.total }} 位成员</span>
      </div>
      <ElTable
        v-loading="loading"
        class="wp-data-table"
        :data="users"
        row-key="id"
      >
        <ElTableColumn
          label="用户名"
          min-width="150"
          prop="username"
        /><ElTableColumn
          label="姓名"
          min-width="150"
          prop="realName"
        /><ElTableColumn label="角色" width="120">
          <template #default="{ row }">
            <ElTag effect="plain">
              {{ row.role === 'admin' ? '管理员' : '编辑' }}
            </ElTag>
          </template> </ElTableColumn
        ><ElTableColumn label="状态" width="110">
          <template #default="{ row }">
            <ElTag :type="row.status === 'active' ? 'success' : 'info'">
              {{ row.status === 'active' ? '启用' : '停用' }}
            </ElTag>
          </template> </ElTableColumn
        ><ElTableColumn label="创建时间" width="180">
          <template #default="{ row }">
            {{ dayjs(row.createdAt).format('YYYY-MM-DD HH:mm') }}
          </template> </ElTableColumn
        ><ElTableColumn align="right" label="操作" width="190">
          <template #default="{ row }">
            <ElButton
              link
              type="primary"
              @click="openEdit(row as SystemUserRecord)"
            >
              编辑 </ElButton
            ><ElButton
              link
              type="warning"
              @click="openReset(row as SystemUserRecord)"
            >
              重置密码
            </ElButton>
          </template> </ElTableColumn
        ><template #empty><ElEmpty description="暂无用户" /></template>
      </ElTable>
      <div class="wp-table-panel__footer">
        <ElPagination
          background
          :current-page="pagination.page"
          layout="total, prev, pager, next"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          @current-change="
            (page) => {
              pagination.page = page;
              load();
            }
          "
        />
      </div>
    </ElCard>
    <ElDialog
      v-model="dialogVisible"
      :title="editingId ? '编辑用户' : '新增用户'"
      width="min(520px, 92vw)"
    >
      <ElForm
        ref="formRef"
        label-position="top"
        :model="form"
        :rules="formRules"
      >
        <ElFormItem label="用户名" prop="username">
          <ElInput
            v-model="form.username"
            :disabled="!!editingId"
          /> </ElFormItem
        ><ElFormItem v-if="!editingId" label="初始密码" prop="password">
          <ElInput
            v-model="form.password"
            show-password
            type="password"
          /> </ElFormItem
        ><ElFormItem label="姓名" prop="realName">
          <ElInput v-model="form.realName" /> </ElFormItem
        ><ElFormItem label="角色" prop="role">
          <ElSelect v-model="form.role" class="w-full">
            <ElOption label="管理员" value="admin" /><ElOption
              label="编辑"
              value="editor"
            />
          </ElSelect> </ElFormItem
        ><ElFormItem v-if="editingId" label="状态">
          <ElSelect v-model="form.status" class="w-full">
            <ElOption label="启用" value="active" /><ElOption
              label="停用"
              value="disabled"
            />
          </ElSelect>
        </ElFormItem> </ElForm
      ><template #footer>
        <ElButton @click="dialogVisible = false">取消</ElButton
        ><ElButton :loading="submitting" type="primary" @click="submit">
          保存
        </ElButton>
      </template>
    </ElDialog>
    <ElDialog v-model="resetVisible" title="重置密码" width="min(420px, 92vw)">
      <p class="mb-3 text-sm">
        为
        {{ resetTarget?.username }} 设置新密码，保存后该用户已有登录会话将失效。
      </p>
      <ElInput v-model="resetPassword" show-password type="password" /><template
        #footer
      >
        <ElButton @click="resetVisible = false">取消</ElButton
        ><ElButton type="primary" @click="confirmReset"> 确认重置 </ElButton>
      </template>
    </ElDialog>
  </div>
</template>
