# php:8.2-apache 默认不含 mysqli/pdo_mysql，镜像构建时编译进去（首次构建约 1-2 分钟，
# 之后走缓存）。比在启动命令里临时 docker-php-ext-install 稳定得多。
FROM php:8.2-apache
RUN docker-php-ext-install mysqli pdo_mysql
