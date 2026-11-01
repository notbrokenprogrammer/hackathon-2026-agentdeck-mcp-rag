import * as THREE from 'three';
import { PointerLockControls } from 'three/addons/PointerLockControls.js';

/**
 * PlayerControls.js
 * Контроллер движения персонажа от первого лица (FPS).
 * Реализует движение на WASD, ускорение на Shift и обзор мышью через PointerLock.
 */
export class PlayerControls {
    constructor(camera, domElement) {
        this.camera = camera;
        this.domElement = domElement;

        this.controls = new PointerLockControls(camera, domElement);
        
        // Флаги нажатия клавиш
        this.moveForward = false;
        this.moveBackward = false;
        this.moveLeft = false;
        this.moveRight = false;
        this.isSprinting = false;

        // Физические параметры
        this.velocity = new THREE.Vector3();
        this.direction = new THREE.Vector3();
        this.playerHeight = 1.7; // Рост глаз игрока над землей

        this.walkSpeed = 8.0;
        this.sprintSpeed = 16.0;

        this.isLocked = false;

        this._initListeners();
 window.addEventListener('blur',()=>this.resetKeys());document.addEventListener('focusin',e=>{if(['INPUT','TEXTAREA','SELECT'].includes(e.target.tagName))this.resetKeys();});
    }

    _initListeners() {
        // Захват/освобождение мыши
        this.controls.addEventListener('lock', () => {
            this.isLocked = true;
            const crosshair = document.getElementById('crosshair');
            if (crosshair) crosshair.classList.add('visible');
        });

        this.controls.addEventListener('unlock', () => {
            this.isLocked = false;
 this.resetKeys();
            const crosshair = document.getElementById('crosshair');
            if (crosshair) crosshair.classList.remove('visible');
        });

        // Слушатели клавиатуры
        window.addEventListener('keydown', (e) => this._onKeyDown(e));
        window.addEventListener('keyup', (e) => this._onKeyUp(e));
    }

    _onKeyDown(event) {
        // Если фокус в инпуте формы, не перехватываем WASD
        if (event.target.tagName === 'INPUT' || event.target.tagName === 'TEXTAREA' || event.target.tagName === 'SELECT' || event.target.isContentEditable) {
            return;
        }

        switch (event.code) {
            case 'ArrowUp':
            case 'KeyW':
                this.moveForward = true;
                break;
            case 'ArrowLeft':
            case 'KeyA':
                this.moveLeft = true;
                break;
            case 'ArrowDown':
            case 'KeyS':
                this.moveBackward = true;
                break;
            case 'ArrowRight':
            case 'KeyD':
                this.moveRight = true;
                break;
            case 'ShiftLeft':
            case 'ShiftRight':
                this.isSprinting = true;
                break;
        }
    }

    _onKeyUp(event) {
        switch (event.code) {
            case 'ArrowUp':
            case 'KeyW':
                this.moveForward = false;
                break;
            case 'ArrowLeft':
            case 'KeyA':
                this.moveLeft = false;
                break;
            case 'ArrowDown':
            case 'KeyS':
                this.moveBackward = false;
                break;
            case 'ArrowRight':
            case 'KeyD':
                this.moveRight = false;
                break;
            case 'ShiftLeft':
            case 'ShiftRight':
                this.isSprinting = false;
                break;
        }
    }

    /**
     * Попытка захвата указателя мыши
     */
    resetKeys(){this.moveForward=this.moveBackward=this.moveLeft=this.moveRight=this.isSprinting=false;this.velocity.set(0,0,0);}
    lock() {
        if (!this.isLocked) {
 this.resetKeys();
            this.controls.lock();
        }
    }

    /**
     * Освобождение мыши
     */
    unlock() {
        if (this.isLocked) {
            this.controls.unlock();
        }
    }

    /**
     * Обновление физики движения за кадр delta (в секундах)
     * @param {number} delta 
     */
    update(delta) {
        if (!this.isLocked) return;

        // Затухание скорости (трение)
        this.velocity.x -= this.velocity.x * 10.0 * delta;
        this.velocity.z -= this.velocity.z * 10.0 * delta;

        // Вектор направления движения
        this.direction.z = Number(this.moveForward) - Number(this.moveBackward);
        this.direction.x = Number(this.moveRight) - Number(this.moveLeft);
        this.direction.normalize();

        const currentSpeed = this.isSprinting ? this.sprintSpeed : this.walkSpeed;

        if (this.moveForward || this.moveBackward) {
            this.velocity.z -= this.direction.z * currentSpeed * 10.0 * delta;
        }
        if (this.moveLeft || this.moveRight) {
            this.velocity.x -= this.direction.x * currentSpeed * 10.0 * delta;
        }

        // Перемещение по осям X и Z в пространстве камеры
        this.controls.moveRight(-this.velocity.x * delta);
        this.controls.moveForward(-this.velocity.z * delta);

        // Удерживаем высоту глаз игрока над землей
        this.camera.position.y = this.playerHeight;
    }

    /**
     * Текущая позиция игрока
     */
    getPosition() {
        return this.camera.position;
    }

    /**
     * Текущий угол обзора (yaw) в радианах
     */
    getYaw() {
        return this.camera.rotation.y;
    }

    /**
     * Установка позиции игрока (например, при спавне от сервера)
     */
    setPosition(x, y, z) {
        this.camera.position.set(x, (y || 0) + this.playerHeight, z);
    }

    /**
     * Установка угла обзора (yaw) в радианах
     */
    setYaw(yaw) {
        if (typeof yaw === 'number' && !isNaN(yaw)) {
            this.camera.rotation.y = yaw;
        }
    }
}
