# 使用 Debian 系镜像：@temporalio/core-bridge 的 native 绑定依赖 glibc，
# Alpine (musl) 缺少 __register_atfork 等符号会导致 ERR_DLOPEN_FAILED
FROM node:20-bookworm-slim

WORKDIR /app

# Ensure we have the latest npm
RUN npm install -g npm@latest

# Copy package files first for better caching
COPY package*.json ./

# Install dependencies
RUN npm install

# Copy the rest of the application
COPY . .

# Run the worker via npm start (which uses ts-node as defined in package.json)
CMD ["npm", "start"]
